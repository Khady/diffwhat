# Diffwhat

Diffwhat is a small tool to take changes from a diff and list semantic
references to the entities changed by those changes. It currently supports
OCaml through `ocamllsp`.

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

ocaml-junit$ diffwhat
References to changed entity Testcase.pass
/home/louis/Code/github/ocaml-junit/ounit/junit_ounit.ml:18:    J.Testcase.pass
/home/louis/Code/github/ocaml-junit/junit/test/simple.ml:59:        Junit.Testcase.pass
/home/louis/Code/github/ocaml-junit/junit/junit.mli:102:  val pass :
/home/louis/Code/github/ocaml-junit/junit/junit.ml:76:  let pass ~name ~classname ~time =
/home/louis/Code/github/ocaml-junit/alcotest/junit_alcotest.ml:21:      Junit.Testcase.pass
```

## Requirements

Install `ocaml-lsp-server` in the switch used by the project being analyzed.
Project-wide references require an up-to-date Dune index:

```bash
opam exec -- dune build @ocaml-index
diffwhat
```

Diffwhat currently opens the changed files and makes the
`textDocument/documentSymbol` and `textDocument/references` requests. It does
not maintain a long-running editor session or subscribe to build events.

## Usage

```bash
diffwhat                    # unstaged changes in the current repository
diffwhat --staged           # staged changes
diffwhat main...HEAD        # a commit range
diffwhat -C path/to/project # another repository
diffwhat --patch < file.diff
```

Unless `--patch` is used, Diffwhat discovers the repository root and invokes
`git diff --unified=0 --no-color --no-ext-diff` itself.

## How to build

```bash
opam switch create . 5.4.1
opam install . --deps-only --with-dev-setup
opam exec -- dune build @runtest
```

The executable can then be installed with `opam install .`.
