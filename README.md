# Diffwhat

Diffwhat takes changes from a diff and lists the OCaml references that may be
affected by those changes. It currently analyzes `.ml` implementation files
only and requires the target OCaml project to be compiled beforehand.

```
ocaml-junit$ dune build
ocaml-junit$ git diff
diff --git a/junit/junit.ml b/junit/junit.ml
index 212339e..707adcf 100644
--- a/junit/junit.ml
+++ b/junit/junit.ml
@@ -74,6 +74,7 @@ module Testcase = struct
     make ~name ~classname ~time Skipped

   let pass ~name ~classname ~time =
+
     make ~name ~classname ~time Pass
 end

ocaml-junit$ git diff -U0 | diffwhat $PWD
Places affected by a change in Junit.Testcase.pass
/home/louis/Code/github/ocaml-junit/ounit/junit_ounit.ml:18:    J.Testcase.pass
/home/louis/Code/github/ocaml-junit/junit/test/simple.ml:59:        Junit.Testcase.pass
/home/louis/Code/github/ocaml-junit/junit/junit.mli:102:  val pass :
/home/louis/Code/github/ocaml-junit/junit/junit.ml:76:  let pass ~name ~classname ~time =
/home/louis/Code/github/ocaml-junit/alcotest/junit_alcotest.ml:21:      Junit.Testcase.pass
```

## Requirements

- Go 1.24 or newer to build Diffwhat
- `ocamlmerlin` from Merlin
- `ocp-grep` from ocp-index
- A compiled target OCaml project, including the Merlin and ocp-index metadata

## Build and test

```bash
opam switch create . 5.4.1
opam install dune merlin ocp-index
go build .
opam exec -- go test ./...
```

The test suite compiles a real OCaml fixture and exercises both
`ocamlmerlin` and `ocp-grep`.

Install it with:

```bash
go install github.com/Khady/diffwhat@latest
```

## Usage

Run Diffwhat from the target project so Merlin and ocp-index can discover that
project's build metadata. Pass the target Git root as the only argument and a
zero-context Git diff on standard input:

```bash
git diff -U0 | diffwhat "$PWD"
```
