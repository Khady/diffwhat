open Lsp

module Sync_io = struct
  type 'a t = 'a

  let return value = value
  let raise exn = Stdlib.raise exn

  module O = struct
    let ( let+ ) value f = f value
    let ( let* ) value f = f value
  end
end

module Channels = struct
  type input = in_channel
  type output = out_channel

  let read_line channel = In_channel.input_line channel

  let read_exactly channel length =
    try Some (really_input_string channel length) with End_of_file -> None

  let write channel chunks =
    List.iter (Out_channel.output_string channel) chunks;
    Out_channel.flush channel
end

module Io = Lsp.Io.Make (Sync_io) (Channels)

type t = {
  input : in_channel;
  output : out_channel;
  error : in_channel;
  workspace : Types.WorkspaceFolder.t;
  opened_documents : (string, unit) Hashtbl.t;
  mutable messages : string list;
  mutable next_id : int;
  mutable closed : bool;
}

let null = `Null

let respond t id request result =
  Server_request.yojson_of_result request result |> Jsonrpc.Response.ok id
  |> fun response -> Io.write t.output (Jsonrpc.Packet.Response response)

let handle_server_request t (request : Jsonrpc.Request.t) =
  match Server_request.of_jsonrpc request with
  | Ok (Server_request.E WorkspaceFolders) ->
      respond t request.id WorkspaceFolders [ t.workspace ]
  | Ok (Server_request.E (WorkspaceConfiguration params)) ->
      let result = List.map (Fun.const null) params.items in
      respond t request.id (WorkspaceConfiguration params) result
  | Ok (Server_request.E (ClientRegisterCapability params)) ->
      respond t request.id (ClientRegisterCapability params) ()
  | Ok (Server_request.E (ClientUnregisterCapability params)) ->
      respond t request.id (ClientUnregisterCapability params) ()
  | Ok (Server_request.E (WorkDoneProgressCreate params)) ->
      respond t request.id (WorkDoneProgressCreate params) ()
  | Ok (Server_request.E (ShowMessageRequest params)) ->
      respond t request.id (ShowMessageRequest params) None
  | Ok (Server_request.E (UnknownRequest (method_, _))) | Error method_ ->
      let response =
        Jsonrpc.Response.error request.id
          (Jsonrpc.Response.Error.make ~code:MethodNotFound
             ~message:("unsupported server request: " ^ method_)
             ())
      in
      Io.write t.output (Jsonrpc.Packet.Response response)
  | Ok _ ->
      let response =
        Jsonrpc.Response.error request.id
          (Jsonrpc.Response.Error.make ~code:MethodNotFound
             ~message:("unsupported server request: " ^ request.method_)
             ())
      in
      Io.write t.output (Jsonrpc.Packet.Response response)

let handle_server_notification t notification =
  match Server_notification.of_jsonrpc notification with
  | Ok (ShowMessage params) -> t.messages <- params.message :: t.messages
  | Ok (LogMessage params) -> t.messages <- params.message :: t.messages
  | Ok _ | Error _ -> ()

let rec await_response t id =
  match Io.read t.input with
  | None -> failwith "language server closed its output unexpectedly"
  | Some (Jsonrpc.Packet.Response response) when Jsonrpc.Id.equal response.id id
    -> (
      match response.result with
      | Ok result -> result
      | Error error -> Jsonrpc.Response.Error.raise error)
  | Some (Jsonrpc.Packet.Request request) ->
      handle_server_request t request;
      await_response t id
  | Some (Jsonrpc.Packet.Notification notification) ->
      handle_server_notification t notification;
      await_response t id
  | Some (Jsonrpc.Packet.Response _)
  | Some (Jsonrpc.Packet.Batch_response _)
  | Some (Jsonrpc.Packet.Batch_call _) ->
      await_response t id

let request t (type result) (request : result Client_request.t) : result =
  t.next_id <- t.next_id + 1;
  let id = `Int t.next_id in
  let packet =
    Client_request.to_jsonrpc_request request ~id |> fun request ->
    Jsonrpc.Packet.Request request
  in
  Io.write t.output packet;
  Client_request.response_of_json request (await_response t id)

let notify t notification =
  Client_notification.to_jsonrpc notification |> fun notification ->
  Jsonrpc.Packet.Notification notification |> Io.write t.output

let create ~root ~command =
  if Array.length command = 0 then invalid_arg "empty language server command";
  let input, output, error =
    let cwd = Sys.getcwd () in
    Fun.protect
      ~finally:(fun () -> Sys.chdir cwd)
      (fun () ->
        Sys.chdir root;
        Unix.open_process_args_full command.(0) command (Unix.environment ()))
  in
  let root_uri = Lsp.Uri.of_path root in
  let workspace =
    Types.WorkspaceFolder.create ~name:(Filename.basename root) ~uri:root_uri
  in
  let t =
    {
      input;
      output;
      error;
      workspace;
      opened_documents = Hashtbl.create 8;
      messages = [];
      next_id = 0;
      closed = false;
    }
  in
  let documentSymbol =
    Types.DocumentSymbolClientCapabilities.create
      ~hierarchicalDocumentSymbolSupport:true ()
  in
  let references = Types.ReferenceClientCapabilities.create () in
  let textDocument =
    Types.TextDocumentClientCapabilities.create ~documentSymbol ~references ()
  in
  let workspace_capabilities =
    Types.WorkspaceClientCapabilities.create ~configuration:true
      ~workspaceFolders:true ()
  in
  let general =
    Types.GeneralClientCapabilities.create
      ~positionEncodings:[ Types.PositionEncodingKind.UTF8; UTF16 ]
      ()
  in
  let capabilities =
    Types.ClientCapabilities.create ~general ~textDocument
      ~workspace:workspace_capabilities ()
  in
  let clientInfo =
    Types.InitializeParams.create_clientInfo ~name:"diffwhat" ()
  in
  let params =
    Types.InitializeParams.create ~capabilities ~clientInfo
      ~processId:(Unix.getpid ()) ~rootUri:root_uri
      ~workspaceFolders:(Some [ workspace ]) ()
  in
  let initialized = request t (Client_request.Initialize params) in
  let enabled = function
    | None | Some (`Bool false) -> false
    | Some _ -> true
  in
  if not (enabled initialized.capabilities.documentSymbolProvider) then
    failwith "language server does not provide document symbols";
  if not (enabled initialized.capabilities.referencesProvider) then
    failwith "language server does not provide references";
  notify t Client_notification.Initialized;
  t

let open_document t ~file ~language_id =
  let uri = Lsp.Uri.of_path file in
  let key = Lsp.Uri.to_string uri in
  if not (Hashtbl.mem t.opened_documents key) then (
    Hashtbl.add t.opened_documents key ();
    let text = In_channel.with_open_bin file In_channel.input_all in
    let textDocument =
      Types.TextDocumentItem.create ~languageId:language_id ~text ~uri
        ~version:1
    in
    let params = Types.DidOpenTextDocumentParams.create ~textDocument in
    notify t (Client_notification.TextDocumentDidOpen params))

let document_symbols t ~file =
  let textDocument =
    Types.TextDocumentIdentifier.create ~uri:(Lsp.Uri.of_path file)
  in
  let params = Types.DocumentSymbolParams.create ~textDocument () in
  request t (Client_request.DocumentSymbol params)

let references t ~file ~position =
  let textDocument =
    Types.TextDocumentIdentifier.create ~uri:(Lsp.Uri.of_path file)
  in
  let context = Types.ReferenceContext.create ~includeDeclaration:true in
  let params =
    Types.ReferenceParams.create ~context ~position ~textDocument ()
  in
  request t (Client_request.TextDocumentReferences params)
  |> Option.value ~default:[]

let take_messages t =
  let messages = List.rev t.messages in
  t.messages <- [];
  messages

let close t =
  if not t.closed then (
    t.closed <- true;
    Hashtbl.iter
      (fun uri () ->
        let textDocument =
          Types.TextDocumentIdentifier.create ~uri:(Lsp.Uri.of_string uri)
        in
        let params = Types.DidCloseTextDocumentParams.create ~textDocument in
        try notify t (Client_notification.TextDocumentDidClose params)
        with _ -> ())
      t.opened_documents;
    (try ignore (request t Client_request.Shutdown) with _ -> ());
    (try notify t Client_notification.Exit with _ -> ());
    ignore (Unix.close_process_full (t.input, t.output, t.error)))
