open Printf

let usage () =
  eprintf "Usage: %s GIT_ROOT < DIFF\n" Sys.argv.(0);
  exit 2

let () =
  if Array.length Sys.argv <> 2 then usage ();
  let git_root = Sys.argv.(1) in
  let diff = Diffwhat_lib.read_input () in
  let changes = Diffwhat_lib.changes git_root diff in
  let changed_functions =
    List.fold_left
      (fun whole_set (file, lines) ->
        let set = Diffwhat_lib.changed_functions_of_file file lines in
        Diffwhat_lib.Functions_set.union set whole_set)
      Diffwhat_lib.Functions_set.empty changes
  in
  Diffwhat_lib.Functions_set.iter
    (fun fun_name ->
      let occurrences = Diffwhat_lib.ocp_grep fun_name in
      printf "Places affected by a change in %s\n" fun_name;
      List.iter print_endline occurrences;
      print_endline "")
    changed_functions
