open Printf

let language_id file =
  if String.ends_with ~suffix:".ml" file then Some "ocaml"
  else if String.ends_with ~suffix:".mli" file then Some "ocaml.interface"
  else None

let source_line file line =
  try
    In_channel.with_open_text file (fun channel ->
        In_channel.input_lines channel |> fun lines ->
        List.nth_opt lines (line - 1) |> Option.value ~default:"")
  with Sys_error _ -> ""

let print_location (location : Model.location) =
  let file = Lsp.Uri.of_string location.uri |> Lsp.Uri.to_path in
  let line = location.range.start.line + 1 in
  printf "%s:%d:%s\n" file line (source_line file line)

let entity_name (entity : Model.entity) =
  String.concat "." (entity.container @ [ entity.name ])

let compare_entity (left : Model.entity) (right : Model.entity) =
  match String.compare (entity_name left) (entity_name right) with
  | 0 -> (
      match String.compare left.uri right.uri with
      | 0 ->
          compare
            ( left.selection_range.start.line,
              left.selection_range.start.character )
            ( right.selection_range.start.line,
              right.selection_range.start.character )
      | order -> order)
  | order -> order

let compare_location (left : Model.location) (right : Model.location) =
  compare
    ( left.uri,
      left.range.start.line,
      left.range.start.character,
      left.range.end_.line,
      left.range.end_.character )
    ( right.uri,
      right.range.start.line,
      right.range.start.character,
      right.range.end_.line,
      right.range.end_.character )

let analyze ~git_root diff =
  let changes = Diffwhat_lib.changes git_root diff in
  let supported =
    List.filter_map
      (fun (file, lines) ->
        Option.map
          (fun language_id -> (file, lines, language_id))
          (language_id file))
      changes
  in
  if supported <> [] then
    let client = Lsp_client.create ~root:git_root ~command:[| "ocamllsp" |] in
    Fun.protect
      ~finally:(fun () -> Lsp_client.close client)
      (fun () ->
        let entities =
          List.concat_map
            (fun (file, lines, language_id) ->
              let provider =
                Lsp_provider.create ~client ~language_id ~provider:"lsp"
              in
              Lsp_provider.open_document provider ~file;
              Lsp_provider.symbols provider ~file |> fun symbols ->
              Diffwhat_lib.changed_entities symbols lines)
            supported
          |> List.sort_uniq compare_entity
        in
        List.iter
          (fun entity ->
            let provider =
              Lsp_provider.create ~client ~language_id:"ocaml" ~provider:"lsp"
            in
            printf "References to changed entity %s\n" (entity_name entity);
            Lsp_provider.references provider entity
            |> List.sort_uniq compare_location
            |> List.iter print_location;
            print_endline "")
          entities;
        Lsp_client.take_messages client
        |> List.iter (fun message -> eprintf "language server: %s\n" message))

let run directory staged patch range =
  match Cli_options.validate ~staged ~patch ~range with
  | Error message -> Error (`Msg message)
  | Ok () -> (
      try
        let git_root = Git.repository_root directory in
        let diff =
          if patch then Diffwhat_lib.read_input ()
          else Git.diff ~root:git_root ~staged ~range
        in
        analyze ~git_root diff;
        Ok ()
      with
      | Git.Error message -> Error (`Msg message)
      | Unix.Unix_error (error, operation, argument) ->
          Error
            (`Msg
               (Printf.sprintf "%s failed for %s: %s" operation argument
                  (Unix.error_message error)))
      | Failure message -> Error (`Msg message))

open Cmdliner

let directory =
  let doc = "Run as if Diffwhat was started in $(docv)." in
  Arg.(value & opt dir (Sys.getcwd ()) & info [ "C" ] ~docv:"DIRECTORY" ~doc)

let staged =
  let doc = "Analyze staged changes instead of unstaged changes." in
  Arg.(value & flag & info [ "staged"; "cached" ] ~doc)

let patch =
  let doc = "Read a unified diff from standard input." in
  Arg.(value & flag & info [ "patch" ] ~doc)

let range =
  let doc = "Analyze changes in the Git revision $(docv)." in
  Arg.(value & pos 0 (some string) None & info [] ~docv:"RANGE" ~doc)

let command =
  let doc = "find semantic references to changed source entities" in
  let info = Cmd.info "diffwhat" ~doc in
  Cmd.v info Term.(term_result (const run $ directory $ staged $ patch $ range))

let () = exit (Cmd.eval command)
