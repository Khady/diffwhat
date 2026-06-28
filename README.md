# Diffwhat

Diffwhat takes changes from a diff and lists references that may be affected by
those changes. It uses the Language Server Protocol (LSP) for source analysis.
It supports OCaml `.ml` implementation files through `ocamllsp` and Go `.go`
files through `gopls`.

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

ocaml-junit$ opam exec -- diffwhat
Places affected by a change in Junit.Testcase.pass
/home/louis/Code/github/ocaml-junit/ounit/junit_ounit.ml:18:    J.Testcase.pass
/home/louis/Code/github/ocaml-junit/junit/test/simple.ml:59:        Junit.Testcase.pass
/home/louis/Code/github/ocaml-junit/junit/junit.mli:102:  val pass :
/home/louis/Code/github/ocaml-junit/junit/junit.ml:76:  let pass ~name ~classname ~time =
/home/louis/Code/github/ocaml-junit/alcotest/junit_alcotest.ml:21:      Junit.Testcase.pass
```

## Requirements

- Go 1.26 or newer to build Diffwhat
- `gopls` when analyzing Go
- `ocamllsp` from `ocaml-lsp-server` and an up-to-date `@ocaml-index` when
  analyzing OCaml

## Build and test

```bash
opam switch create . 5.4.1
opam install dune ocaml-lsp-server
go install golang.org/x/tools/gopls@latest
go build .
opam exec -- go test ./...
```

The test suite exercises `textDocument/documentSymbol` and
`textDocument/references` against real language servers. OCaml coverage
includes nested modules, wrapped Dune libraries, module aliases, local aliases
and opens, `include` re-exports, and functor results. Go coverage includes
cross-package and cross-module references, pointer and promoted methods,
interfaces, concrete implementations, generics, and `_test.go` consumers.

Install it with:

```bash
go install github.com/Khady/diffwhat@latest
```

## Usage

For OCaml projects, build the target project's index first:

```bash
dune build @ocaml-index
```

Run Diffwhat from the target repository. By default it analyzes unstaged
changes. Use `opam exec --` when the repository contains OCaml; for a Go-only
repository, invoke `diffwhat` directly:

```bash
opam exec -- diffwhat
diffwhat
```

Other Git modes are available directly:

```bash
opam exec -- diffwhat --staged
opam exec -- diffwhat main...HEAD
opam exec -- diffwhat -C path/to/project
```

To analyze an externally produced patch, use:

```bash
git show HEAD | opam exec -- diffwhat --patch
```

Diffwhat discovers the Git root, obtains a zero-context diff, and starts the
required language servers in that root. `gopls` supports both `go.mod` and
`go.work` workspaces. Untracked files are excluded, matching `git diff`. A
stale OCaml index can produce incomplete reference results; OCaml-LSP reports
that condition as a warning.
