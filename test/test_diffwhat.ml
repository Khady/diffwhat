let changes = Alcotest.(list (pair string (list int)))
let functions = Alcotest.(list (triple string int int))
let hunk = Alcotest.(option (pair int int))

let test_single_hunk () =
  let root = "/project" in
  let diff = [ "+++ b/lib/example.ml"; "@@ -7,2 +10,3 @@" ] in
  Alcotest.check changes "changed lines"
    [ ("/project/lib/example.ml", [ 10; 11; 12 ]) ]
    (Diffwhat_lib.changes root diff)

let test_multiple_hunks () =
  let root = "/project" in
  let diff =
    [
      "+++ b/lib/example.ml";
      "@@ -7 +10 @@";
      "@@ -20,2 +30,2 @@";
      "+++ b/README.md";
      "@@ -1 +1 @@";
    ]
  in
  Alcotest.check changes "changed lines"
    [ ("/project/lib/example.ml", [ 10; 30; 31 ]) ]
    (Diffwhat_lib.changes root diff)

let test_parse_hunk () =
  Alcotest.check hunk "default length"
    (Some (10, 1))
    (Diffwhat_lib.parse_hunk "@@ -7 +10 @@");
  Alcotest.check hunk "explicit length"
    (Some (30, 2))
    (Diffwhat_lib.parse_hunk "@@ -20,2 +30,2 @@ let example =");
  Alcotest.check hunk "invalid" None (Diffwhat_lib.parse_hunk "@@ invalid @@")

let position line = Diffwhat_lib.{ line; col = 0 }

let value ?(children = []) kind name start end_ =
  Diffwhat_lib.
    { start = position start; end_ = position end_; name; kind; children }

let test_nested_functions () =
  let outline =
    [
      value Value "top_level" 1 2;
      value Module "Nested" 3 8 ~children:[ value Value "inner" 4 7 ];
      value Other "ignored" 9 10;
    ]
  in
  Alcotest.check functions "function paths"
    [ ("Example.Nested.inner", 4, 7); ("Example.top_level", 1, 2) ]
    (Diffwhat_lib.functions outline "/project/example.ml")

let test_outline_json () =
  let json =
    {|{"class":"return","value":[{"start":{"line":1,"col":0},"end":{"line":2,"col":3},"name":"run","kind":"Value","children":[]},{"start":{"line":3,"col":0},"end":{"line":3,"col":4},"name":"E","kind":"Exn","children":[]}]}|}
  in
  Alcotest.check functions "decoded outline"
    [ ("Example.run", 1, 2) ]
    (Diffwhat_lib.functions (Diffwhat_lib.outline_of_string json) "example.ml")

let () =
  Alcotest.run "diffwhat"
    [
      ( "changes",
        [
          Alcotest.test_case "single hunk" `Quick test_single_hunk;
          Alcotest.test_case "multiple hunks" `Quick test_multiple_hunks;
          Alcotest.test_case "parse hunk" `Quick test_parse_hunk;
        ] );
      ( "functions",
        [
          Alcotest.test_case "nested modules" `Quick test_nested_functions;
          Alcotest.test_case "decode JSON" `Quick test_outline_json;
        ] );
    ]
