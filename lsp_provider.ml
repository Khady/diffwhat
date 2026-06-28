open Lsp.Types

type t = { client : Lsp_client.t; language_id : string; provider : string }

let create ~client ~language_id ~provider = { client; language_id; provider }

let open_document t ~file =
  Lsp_client.open_document t.client ~file ~language_id:t.language_id

let kind_of_lsp = function
  | SymbolKind.Function -> Model.Function
  | Method -> Method
  | Constructor -> Constructor
  | Variable | Constant -> Value
  | Module | Namespace | Package -> Module
  | Class | Interface | Enum | Struct | TypeParameter -> Type
  | _ -> Other

let document_entity t ~uri ~container (symbol : DocumentSymbol.t) =
  {
    Model.uri;
    name = symbol.name;
    container;
    kind = kind_of_lsp symbol.kind;
    range = Model.range_of_lsp symbol.range;
    selection_range = Model.range_of_lsp symbol.selectionRange;
    provider = t.provider;
  }

let rec flatten_document_symbols t ~uri ~container symbols =
  List.concat_map
    (fun (symbol : DocumentSymbol.t) ->
      let entity = document_entity t ~uri ~container symbol in
      let children =
        flatten_document_symbols t ~uri
          ~container:(container @ [ symbol.name ])
          (Option.value ~default:[] symbol.children)
      in
      entity :: children)
    symbols

let file_module file =
  file |> Filename.basename |> Filename.remove_extension
  |> String.capitalize_ascii

let symbol_information_entity t (symbol : SymbolInformation.t) =
  {
    Model.uri = Lsp.Uri.to_string symbol.location.uri;
    name = symbol.name;
    container =
      Option.fold ~none:[] ~some:(fun name -> [ name ]) symbol.containerName;
    kind = kind_of_lsp symbol.kind;
    range = Model.range_of_lsp symbol.location.range;
    selection_range = Model.range_of_lsp symbol.location.range;
    provider = t.provider;
  }

let symbols t ~file =
  match Lsp_client.document_symbols t.client ~file with
  | None -> []
  | Some (`DocumentSymbol symbols) ->
      flatten_document_symbols t
        ~uri:(Lsp.Uri.of_path file |> Lsp.Uri.to_string)
        ~container:
          (if String.starts_with ~prefix:"ocaml" t.language_id then
             [ file_module file ]
           else [])
        symbols
  | Some (`SymbolInformation symbols) ->
      List.map (symbol_information_entity t) symbols

let references t (entity : Model.entity) =
  let position =
    Position.create ~line:entity.Model.selection_range.start.line
      ~character:entity.selection_range.start.character
  in
  Lsp_client.references t.client
    ~file:(Lsp.Uri.of_string entity.uri |> Lsp.Uri.to_path)
    ~position
  |> List.map Model.location_of_lsp
