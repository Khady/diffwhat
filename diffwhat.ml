open Printf
module Functions_set = Set.Make (String)

let merlin_outline file =
  let cmd =
    sprintf
      "ocamlmerlin single outline -protocol json -verbosity 0 -filename %s < %s"
      file file
  in
  let ic = Unix.open_process_in cmd in
  let json = ExtLib.input_all ic in
  let outline = Merlin_outline_j.doc_of_string json in
  outline.value

let function_path name mod_path =
  match mod_path with
  | [] -> name
  | p -> sprintf "%s.%s" (p |> List.rev |> String.concat ".") name

let module_of_file file =
  let basename = Filename.basename file in
  let name = Filename.remove_extension basename in
  String.capitalize_ascii name

let functions outline file =
  let open Merlin_outline_t in
  let mod_name = module_of_file file in
  let rec fold (funs, mod_path) values =
    match values with
    | [] -> funs
    | value :: values -> (
        match value with
        | {
         kind = `Value;
         name;
         start = { line = start; _ };
         end_ = { line = end_; _ };
         _;
        } ->
            let f = (function_path name mod_path, start, end_) in
            fold (f :: funs, mod_path) values
        | { kind = `Module; name; children; _ } ->
            let children_funs = fold ([], name :: mod_path) children in
            fold (children_funs @ funs, mod_path) values
        | { kind = `Type | `Label | `Constructor; _ } ->
            fold (funs, mod_path) values )
  in
  fold ([], [ mod_name ]) outline

let ocp_grep fun_path =
  let cmd = sprintf "ocp-grep %s" fun_path in
  let ic = Unix.open_process_in cmd in
  let occurrences = ExtLib.input_lines ic in
  ExtLib.List.of_enum occurrences

let hunk_tyre =
  Tyre.(
    str "@@ -" *> int *> opt (str "," <&> int) *> str " +" *> int
    <&> opt (str "," *> int)
    <* str " @@")

let hunk_re = Tyre.compile hunk_tyre

let line_numbers_of_hunk start len = List.init len (fun i -> start + i)

let changes git_root diff =
  let positions, current =
    List.fold_left
      (fun (positions, current) line ->
        let new_file =
          match ExtLib.String.starts_with line "+++" with
          | false -> None
          | true ->
              let prefix_len = String.length "+++ b/" in
              let f =
                String.sub line prefix_len (String.length line - prefix_len)
              in
              Some (Filename.concat git_root f)
        in
        let positions, current =
          match new_file with
          | None -> (positions, current)
          | Some f -> (
              let new_current =
                (* we only handle ml files *)
                match ExtLib.String.ends_with f ".ml" with
                | true -> Some (f, [])
                | false -> None
              in
              match current with
              | None -> (positions, new_current)
              | Some (f, lines) ->
                  let l = lines |> List.rev |> List.concat in
                  ((f, l) :: positions, new_current) )
        in
        let new_hunk =
          match current with
          | None -> None
          | Some (file, _) -> (
              match ExtLib.String.starts_with line "@@" with
              | false -> None
              | true -> (
                  match Tyre.exec hunk_re line with
                  | Ok (start, len) ->
                      let len = Option.default 1 len in
                      let lines = line_numbers_of_hunk start len in
                      Some lines
                  | Error e ->
                      Format.printf "unable to read hunk line for file %s: %a"
                        file Tyre.pp_error e;
                      None ) )
        in
        let current =
          match new_hunk with
          | None -> current
          | Some new_hunk -> (
              match current with
              | None -> None
              | Some (f, lines) -> Some (f, new_hunk :: lines) )
        in
        (positions, current))
      ([], None) diff
  in
  match current with
  | None -> positions
  | Some (f, lines) ->
      let l = lines |> List.rev |> List.concat in
      (f, l) :: positions

let print_changes changes =
  List.iter
    (fun (f, l) -> List.iter (fun n -> printf "file %s %d\n" f n) l)
    changes

let changed_functions_of_file file lines =
  let outlines = merlin_outline file in
  let functions = functions outlines file in
  let functions =
    List.sort
      (fun (_, start_a, _) (_, start_b, _) -> Int.compare start_a start_b)
      functions
  in
  let changed_functions =
    List.fold_left
      (fun set (name, start, end_) ->
        match List.exists (fun line -> start <= line && line <= end_) lines with
        | false -> set
        | true -> Functions_set.add name set)
      Functions_set.empty functions
  in
  changed_functions

let read_input () = ExtLib.(List.of_enum (input_lines stdin))

let () =
  let git_root = Sys.argv.(1) in
  let diff = read_input () in
  let changes = changes git_root diff in
  let changed_functions =
    List.fold_left
      (fun whole_set (file, lines) ->
        let set = changed_functions_of_file file lines in
        Functions_set.union set whole_set)
      Functions_set.empty changes
  in
  Functions_set.iter
    (fun fun_name ->
      let occurrences = ocp_grep fun_name in
      printf "Places affected by a change in %s\n" fun_name;
      List.iter print_endline occurrences;
      print_endline "")
    changed_functions;
  ()
