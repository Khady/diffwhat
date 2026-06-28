# Diffwhat

Diffwhat is a small tool to take changes from a diff and list all the places
that are affected by those changes. It works only for ocaml. And it requires
the project to be compiled beforehand.

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

## How to build

Diffwhat currently targets OCaml 5.4. `ocp-index`, which provides the
`ocp-grep` command used by Diffwhat, does not yet support OCaml 5.5.

```bash
opam switch create . 5.4.1
opam install . --deps-only --with-dev-setup
opam exec -- dune build @runtest
```

The executable can then be installed with `opam install .`.
