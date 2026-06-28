let changes git_root diff =
  let normalize_hunk_header line =
    match String.split_on_char ' ' line with
    | "@@" :: old_range :: new_range :: rest ->
        let explicit_length range =
          if String.contains range ',' then range else range ^ ",1"
        in
        String.concat " "
          ("@@" :: explicit_length old_range :: explicit_length new_range
         :: rest)
    | _ -> line
  in
  let data =
    diff |> List.map normalize_hunk_header |> String.concat "\n" |> fun data ->
    data ^ "\n"
  in
  let patches =
    try Patch.parse ~p:1 data with
    | Patch.Parse_error error -> failwith ("unable to parse diff: " ^ error.msg)
    | Invalid_argument message -> failwith ("unable to parse diff: " ^ message)
  in
  List.filter_map
    (fun (patch : Patch.t) ->
      let path =
        match patch.operation with
        | Edit (_, path) | Create path -> Some path
        | Git_ext (_, path, (Rename_only _ | Create_only)) -> Some path
        | Delete _ | Git_ext (_, _, Delete_only) -> None
      in
      Option.bind path (fun path ->
          let lines =
            List.concat_map
              (fun (hunk : Patch.hunk) ->
                List.init hunk.their_len (fun offset ->
                    hunk.their_start + offset))
              patch.hunks
          in
          if lines = [] then None
          else Some (Filename.concat git_root path, lines)))
    patches

let changed_entities symbols lines =
  let reportable (entity : Model.entity) =
    match entity.kind with
    | Function | Method | Constructor | Value | Type -> true
    | Module | Other -> false
  in
  symbols |> List.filter reportable
  |> List.filter (fun (entity : Model.entity) ->
      Model.range_intersects_lines entity.range lines)

let read_input () = In_channel.input_lines stdin
