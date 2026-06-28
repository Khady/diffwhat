exception Error of string

let run ~directory arguments =
  let command = Array.of_list ("git" :: "-C" :: directory :: arguments) in
  let stdout, stdin, stderr =
    Unix.open_process_args_full "git" command (Unix.environment ())
  in
  let output = In_channel.input_all stdout in
  let error = In_channel.input_all stderr in
  match Unix.close_process_full (stdout, stdin, stderr) with
  | WEXITED 0 -> output
  | WEXITED code ->
      raise
        (Error
           (Printf.sprintf "git exited with status %d: %s" code
              (String.trim error)))
  | WSIGNALED signal ->
      raise
        (Error
           (Printf.sprintf "git was killed by signal %d: %s" signal
              (String.trim error)))
  | WSTOPPED signal ->
      raise
        (Error
           (Printf.sprintf "git was stopped by signal %d: %s" signal
              (String.trim error)))

let repository_root directory =
  run ~directory [ "rev-parse"; "--show-toplevel" ] |> String.trim

let diff_arguments ~staged ~range =
  [ "diff"; "--unified=0"; "--no-color"; "--no-ext-diff" ]
  @ (if staged then [ "--cached" ] else [])
  @ Option.fold ~none:[] ~some:(fun range -> [ range ]) range
  @ [ "--" ]

let diff ~root ~staged ~range =
  run ~directory:root (diff_arguments ~staged ~range)
  |> String.split_on_char '\n'
