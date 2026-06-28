open Printf
module Functions_set = Set.Make (String)

type position = { line : int; col : int }
type outline_kind = Value | Module | Other

type outline = {
  start : position;
  end_ : position;
  name : string;
  kind : outline_kind;
  children : outline list;
}

let check_process_status command = function
  | Unix.WEXITED 0 -> ()
  | Unix.WEXITED code ->
      failwith (sprintf "%s exited with status %d" command code)
  | Unix.WSIGNALED signal ->
      failwith (sprintf "%s was killed by signal %d" command signal)
  | Unix.WSTOPPED signal ->
      failwith (sprintf "%s was stopped by signal %d" command signal)

let position_of_json json =
  let open Yojson.Safe.Util in
  {
    line = json |> member "line" |> to_int;
    col = json |> member "col" |> to_int;
  }

let outline_kind_of_json json =
  match Yojson.Safe.Util.to_string json with
  | "Value" -> Value
  | "Module" -> Module
  | _ -> Other

let rec outline_of_json json =
  let open Yojson.Safe.Util in
  {
    start = json |> member "start" |> position_of_json;
    end_ = json |> member "end" |> position_of_json;
    name = json |> member "name" |> to_string;
    kind = json |> member "kind" |> outline_kind_of_json;
    children = json |> member "children" |> to_list |> List.map outline_of_json;
  }

let outline_of_string json =
  let open Yojson.Safe.Util in
  json |> Yojson.Safe.from_string |> member "value" |> to_list
  |> List.map outline_of_json

let merlin_outline file =
  let cmd =
    sprintf
      "ocamlmerlin single outline -protocol json -verbosity 0 -filename %s < %s"
      (Filename.quote file) (Filename.quote file)
  in
  let ic = Unix.open_process_in cmd in
  let json = In_channel.input_all ic in
  check_process_status "ocamlmerlin" (Unix.close_process_in ic);
  outline_of_string json

let function_path name mod_path =
  match mod_path with
  | [] -> name
  | p -> sprintf "%s.%s" (p |> List.rev |> String.concat ".") name

let module_of_file file =
  let basename = Filename.basename file in
  let name = Filename.remove_extension basename in
  String.capitalize_ascii name

let functions outline file =
  let mod_name = module_of_file file in
  let rec fold (funs, mod_path) values =
    match values with
    | [] -> funs
    | value :: values -> (
        match value with
        | {
         kind = Value;
         name;
         start = { line = start; _ };
         end_ = { line = end_; _ };
         _;
        } ->
            let f = (function_path name mod_path, start, end_) in
            fold (f :: funs, mod_path) values
        | { kind = Module; name; children; _ } ->
            let children_funs = fold ([], name :: mod_path) children in
            fold (children_funs @ funs, mod_path) values
        | { kind = Other; _ } -> fold (funs, mod_path) values)
  in
  fold ([], [ mod_name ]) outline

let ocp_grep fun_path =
  let ic = Unix.open_process_args_in "ocp-grep" [| "ocp-grep"; fun_path |] in
  let lines = In_channel.input_lines ic in
  check_process_status "ocp-grep" (Unix.close_process_in ic);
  lines

let line_numbers_of_hunk start len = List.init len (fun i -> start + i)

let parse_hunk line =
  let parse_range range =
    match String.split_on_char ',' range with
    | [ start ] ->
        Option.map (fun start -> (start, 1)) (int_of_string_opt start)
    | [ start; length ] -> (
        match (int_of_string_opt start, int_of_string_opt length) with
        | Some start, Some length -> Some (start, length)
        | _ -> None)
    | _ -> None
  in
  match String.index_opt line '+' with
  | None -> None
  | Some plus ->
      let range_start = plus + 1 in
      let range_end =
        Option.value ~default:(String.length line)
          (String.index_from_opt line range_start ' ')
      in
      String.sub line range_start (range_end - range_start) |> parse_range

let changes git_root diff =
  let positions, current =
    List.fold_left
      (fun (positions, current) line ->
        let new_file =
          match String.starts_with ~prefix:"+++" line with
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
                match String.ends_with ~suffix:".ml" f with
                | true -> Some (f, [])
                | false -> None
              in
              match current with
              | None -> (positions, new_current)
              | Some (f, lines) ->
                  let l = lines |> List.rev |> List.concat in
                  ((f, l) :: positions, new_current))
        in
        let new_hunk =
          match current with
          | None -> None
          | Some (file, _) -> (
              match String.starts_with ~prefix:"@@" line with
              | false -> None
              | true -> (
                  match parse_hunk line with
                  | Some (start, len) ->
                      let lines = line_numbers_of_hunk start len in
                      Some lines
                  | None ->
                      eprintf "unable to read hunk line for file %s: %s\n" file
                        line;
                      None))
        in
        let current =
          match new_hunk with
          | None -> current
          | Some new_hunk -> (
              match current with
              | None -> None
              | Some (f, lines) -> Some (f, new_hunk :: lines))
        in
        (positions, current))
      ([], None) diff
  in
  match current with
  | None -> positions
  | Some (f, lines) ->
      let l = lines |> List.rev |> List.concat in
      (f, l) :: positions

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

let read_input () = In_channel.input_lines stdin
