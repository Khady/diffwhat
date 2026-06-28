# Diffwhat

Diffwhat takes changes from a diff and lists references that may be affected by
those changes. It uses the Language Server Protocol (LSP) for source analysis.
The first supported language is OCaml, currently limited to `.ml`
implementation files.

```
ocaml-junit$ dune build @ocaml-index
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
- `ocamllsp` from `ocaml-lsp-server`
- An up-to-date `@ocaml-index` for the target OCaml project

## Build and test

```bash
opam switch create . 5.4.1
opam install dune ocaml-lsp-server
go build .
opam exec -- go test ./...
```

The test suite compiles a real OCaml fixture and exercises both
`textDocument/documentSymbol` and `textDocument/references` through
`ocamllsp`.

Install it with:

```bash
go install github.com/Khady/diffwhat@latest
```

## Usage

Build the target project's OCaml index first:

```bash
dune build @ocaml-index
```

Then pass the target Git root as the only argument and a zero-context Git diff
on standard input:

```bash
git diff -U0 | diffwhat "$PWD"
```

Diffwhat starts the language server in the supplied Git root. A stale index can
produce incomplete reference results; OCaml-LSP reports that condition as a
warning.
