let changes = Alcotest.(list (pair string (list int)))
let diff_lines diff = String.split_on_char '\n' diff

let test_single_hunk () =
  let root = "/project" in
  let diff =
    {|diff --git a/lib/example.ml b/lib/example.ml
--- a/lib/example.ml
+++ b/lib/example.ml
@@ -7,2 +10,3 @@
-old one
-old two
+new one
+new two
+new three|}
    |> diff_lines
  in
  Alcotest.check changes "changed lines"
    [ ("/project/lib/example.ml", [ 10; 11; 12 ]) ]
    (Diffwhat_lib.changes root diff)

let test_multiple_hunks () =
  let root = "/project" in
  let diff =
    {|diff --git a/lib/example.ml b/lib/example.ml
--- a/lib/example.ml
+++ b/lib/example.ml
@@ -7 +10 @@
-old
+new
@@ -20,2 +30,2 @@
-old one
-old two
+new one
+new two
diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1 +1 @@
-old
+new|}
    |> diff_lines
  in
  Alcotest.check changes "changed lines"
    [
      ("/project/lib/example.ml", [ 10; 30; 31 ]); ("/project/README.md", [ 1 ]);
    ]
    (Diffwhat_lib.changes root diff)

let test_deleted_and_renamed_files () =
  let root = "/project" in
  let diff =
    {|diff --git a/lib/deleted.ml b/lib/deleted.ml
deleted file mode 100644
--- a/lib/deleted.ml
+++ /dev/null
@@ -7 +0,0 @@
-deleted
diff --git "a/lib/old name.ml" "b/lib/new name.ml"
similarity index 80%
rename from lib/old name.ml
rename to lib/new name.ml
--- "a/lib/old name.ml"
+++ "b/lib/new name.ml"
@@ -1 +1 @@
-old
+new|}
    |> diff_lines
  in
  Alcotest.check changes "ignore deletion and use renamed destination"
    [ ("/project/lib/new name.ml", [ 1 ]) ]
    (Diffwhat_lib.changes root diff)

let model_position line character = Model.{ line; character }

let model_range start_line end_line =
  Model.
    { start = model_position start_line 0; end_ = model_position end_line 0 }

let test_changed_entities () =
  let entity name kind start_line end_line =
    Model.
      {
        uri = "file:///project/example.ml";
        name;
        container = [];
        kind;
        range = model_range start_line end_line;
        selection_range = model_range start_line start_line;
        provider = "test";
      }
  in
  let symbols =
    [
      entity "changed" Model.Function 4 8;
      entity "unchanged" Model.Function 10 12;
      entity "container" Model.Module 1 20;
    ]
  in
  let actual = Diffwhat_lib.changed_entities symbols [ 6 ] in
  Alcotest.(check (list string))
    "only reportable intersecting entities" [ "changed" ]
    (List.map (fun (entity : Model.entity) -> entity.name) actual)

let test_range_end_is_exclusive () =
  let range = Model.{ start = model_position 2 4; end_ = model_position 5 0 } in
  Alcotest.(check bool)
    "last included line" true
    (Model.range_intersects_lines range [ 5 ]);
  Alcotest.(check bool)
    "exclusive end line" false
    (Model.range_intersects_lines range [ 6 ])

let write_file file contents =
  Out_channel.with_open_text file (fun channel ->
      Out_channel.output_string channel contents)

let git repository arguments = ignore (Git.run ~directory:repository arguments)

let test_git_modes () =
  let root = Filename.temp_dir "diffwhat" "git" in
  git root [ "init"; "-q" ];
  let readme = Filename.concat root "README.md" in
  write_file readme "baseline\n";
  git root [ "add"; "README.md" ];
  git root
    [
      "-c";
      "user.name=Diffwhat";
      "-c";
      "user.email=diffwhat@example.invalid";
      "commit";
      "-q";
      "-m";
      "baseline";
    ];
  let nested = Filename.concat root "nested" in
  Unix.mkdir nested 0o755;
  Alcotest.(check string)
    "repository discovery" root
    (Git.repository_root nested);
  write_file readme "changed\n";
  Alcotest.check changes "unstaged changes"
    [ (readme, [ 1 ]) ]
    (Git.diff ~root ~staged:false ~range:None |> Diffwhat_lib.changes root);
  git root [ "add"; "README.md" ];
  Alcotest.check changes "clean unstaged changes" []
    (Git.diff ~root ~staged:false ~range:None |> Diffwhat_lib.changes root);
  Alcotest.check changes "staged changes"
    [ (readme, [ 1 ]) ]
    (Git.diff ~root ~staged:true ~range:None |> Diffwhat_lib.changes root);
  git root
    [
      "-c";
      "user.name=Diffwhat";
      "-c";
      "user.email=diffwhat@example.invalid";
      "commit";
      "-q";
      "-m";
      "change";
    ];
  Alcotest.check changes "revision range"
    [ (readme, [ 1 ]) ]
    (Git.diff ~root ~staged:false ~range:(Some "HEAD~1..HEAD")
    |> Diffwhat_lib.changes root)

let test_cli_validation () =
  let error = Cli_options.validate ~staged:true ~patch:true ~range:None in
  Alcotest.(check (result unit string))
    "patch and staged"
    (Error "--patch cannot be combined with --staged or a revision") error;
  Alcotest.(check (result unit string))
    "patch and revision"
    (Error "--patch cannot be combined with --staged or a revision")
    (Cli_options.validate ~staged:false ~patch:true ~range:(Some "HEAD"))

let rec project_root directory =
  if Sys.file_exists (Filename.concat directory "dune-project") then directory
  else
    let parent = Filename.dirname directory in
    if parent = directory then Alcotest.fail "unable to find dune-project"
    else project_root parent

let test_ocamllsp () =
  let root = project_root (Sys.getcwd ()) in
  let file = Filename.concat root "test/fixture/target.ml" in
  let client = Lsp_client.create ~root ~command:[| "ocamllsp" |] in
  Fun.protect
    ~finally:(fun () -> Lsp_client.close client)
    (fun () ->
      Lsp_client.open_document client ~file ~language_id:"ocaml";
      let provider =
        Lsp_provider.create ~client ~language_id:"ocaml" ~provider:"lsp"
      in
      let entity =
        Lsp_provider.symbols provider ~file
        |> List.find_opt (fun (entity : Model.entity) ->
            entity.container = [ "Target"; "Increment" ]
            && entity.name = "transform")
        |> Option.get
      in
      let references = Lsp_provider.references provider entity in
      let locations =
        List.map
          (fun (location : Model.location) ->
            ( Lsp.Uri.of_string location.uri
              |> Lsp.Uri.to_path |> Filename.basename,
              location.range.start.line + 1 ))
          references
        |> List.sort compare
      in
      Alcotest.(check (list (pair string int)))
        "compiler-resolved references"
        [
          ("consumer.ml", 1);
          ("consumer.ml", 2);
          ("target.ml", 10);
          ("target.ml", 21);
          ("target.ml", 22);
        ]
        locations)

let test_ocamllsp_complex_layouts () =
  let root = project_root (Sys.getcwd ()) in
  let file = Filename.concat root "test/complex/lib/math.ml" in
  let client = Lsp_client.create ~root ~command:[| "ocamllsp" |] in
  Fun.protect
    ~finally:(fun () -> Lsp_client.close client)
    (fun () ->
      Lsp_client.open_document client ~file ~language_id:"ocaml";
      let provider =
        Lsp_provider.create ~client ~language_id:"ocaml" ~provider:"lsp"
      in
      let symbols = Lsp_provider.symbols provider ~file in
      let entity container name =
        symbols
        |> List.find_opt (fun (entity : Model.entity) ->
            entity.container = container && entity.name = name)
        |> Option.get
      in
      let locations entity =
        Lsp_provider.references provider entity
        |> List.map (fun (location : Model.location) ->
            ( Lsp.Uri.of_string location.uri
              |> Lsp.Uri.to_path |> Filename.basename,
              location.range.start.line + 1 ))
        |> List.sort compare
      in
      Alcotest.(check (list (pair string int)))
        "nested module, include, alias and local-open references"
        [
          ("consumers.ml", 1);
          ("consumers.ml", 5);
          ("consumers.ml", 9);
          ("main.ml", 1);
          ("main.ml", 2);
          ("main.ml", 3);
          ("math.ml", 2);
          ("math.ml", 13);
          ("reexport.ml", 4);
          ("reexport.ml", 5);
        ]
        (entity [ "Math"; "Nested" ] "transform" |> locations);
      Alcotest.(check (list (pair string int)))
        "functor result references"
        [ ("functor_ops.ml", 7); ("main.ml", 4); ("math.ml", 10) ]
        (entity [ "Math"; "Make" ] "run" |> locations))

let run_diffwhat root patch =
  let executable = Sys.getenv "DIFFWHAT_EXE" in
  let input = Filename.temp_file "diffwhat" ".patch" in
  let output = Filename.temp_file "diffwhat" ".out" in
  let error = Filename.temp_file "diffwhat" ".err" in
  write_file input patch;
  let input_fd = Unix.openfile input [ O_RDONLY ] 0 in
  let output_fd = Unix.openfile output [ O_WRONLY; O_TRUNC ] 0o600 in
  let error_fd = Unix.openfile error [ O_WRONLY; O_TRUNC ] 0o600 in
  let arguments = [| executable; "-C"; root; "--patch" |] in
  let pid =
    Unix.create_process executable arguments input_fd output_fd error_fd
  in
  List.iter Unix.close [ input_fd; output_fd; error_fd ];
  let _, status = Unix.waitpid [] pid in
  let stdout = In_channel.with_open_text output In_channel.input_all in
  let stderr = In_channel.with_open_text error In_channel.input_all in
  match status with
  | WEXITED 0 -> stdout
  | _ -> Alcotest.fail ("diffwhat failed: " ^ stderr)

let test_full_pipeline () =
  let root = project_root (Sys.getcwd ()) in
  let patch =
    {|diff --git a/test/complex/lib/math.ml b/test/complex/lib/math.ml
--- a/test/complex/lib/math.ml
+++ b/test/complex/lib/math.ml
@@ -2 +2 @@
-  let transform value = value * 2
+  let transform value = value * 3
@@ -10 +10 @@
-  let run value = Operation.apply value
+  let run value = Operation.apply (value + 1)|}
  in
  let output = run_diffwhat root patch in
  let has_substring substring =
    let substring_length = String.length substring in
    let rec search offset =
      if offset + substring_length > String.length output then false
      else if String.sub output offset substring_length = substring then true
      else search (offset + 1)
    in
    search 0
  in
  let contains text = Alcotest.(check bool) text true (has_substring text) in
  contains "References to changed entity Math.Nested.transform";
  contains "consumers.ml:1:";
  contains "References to changed entity Math.Make.run";
  contains "functor_ops.ml:7:";
  contains "main.ml:4:"

let () =
  Alcotest.run "diffwhat"
    [
      ( "changes",
        [
          Alcotest.test_case "single hunk" `Quick test_single_hunk;
          Alcotest.test_case "multiple hunks" `Quick test_multiple_hunks;
          Alcotest.test_case "deleted and renamed files" `Quick
            test_deleted_and_renamed_files;
        ] );
      ( "entities",
        [
          Alcotest.test_case "changed ranges" `Quick test_changed_entities;
          Alcotest.test_case "exclusive range end" `Quick
            test_range_end_is_exclusive;
        ] );
      ("git", [ Alcotest.test_case "diff modes" `Quick test_git_modes ]);
      ( "cli",
        [ Alcotest.test_case "option validation" `Quick test_cli_validation ] );
      ( "lsp",
        [
          Alcotest.test_case "real ocamllsp" `Slow test_ocamllsp;
          Alcotest.test_case "complex OCaml layouts" `Slow
            test_ocamllsp_complex_layouts;
        ] );
      ( "pipeline",
        [ Alcotest.test_case "real executable" `Slow test_full_pipeline ] );
    ]
