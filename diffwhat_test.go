package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestChangesSingleHunk(t *testing.T) {
	diff := strings.NewReader(`diff --git a/lib/example.ml b/lib/example.ml
--- a/lib/example.ml
+++ b/lib/example.ml
@@ -7,2 +10,3 @@
-old one
-old two
+new one
+new two
+new three
`)
	want := []fileChanges{{
		Path:  "/project/lib/example.ml",
		Lines: []int{10, 11, 12},
	}}

	got, err := changes("/project", diff)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes() = %#v, want %#v", got, want)
	}
}

func TestChangesMultipleHunksAndFiles(t *testing.T) {
	diff := strings.NewReader(`diff --git a/lib/first.ml b/lib/first.ml
--- a/lib/first.ml
+++ b/lib/first.ml
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
+new
diff --git a/lib/second.ml b/lib/second.ml
--- a/lib/second.ml
+++ b/lib/second.ml
@@ -3 +4 @@
-old
+new
`)
	want := []fileChanges{
		{Path: "/project/lib/first.ml", Lines: []int{10, 30, 31}},
		{Path: "/project/README.md", Lines: []int{1}},
		{Path: "/project/lib/second.ml", Lines: []int{4}},
	}

	got, err := changes("/project", diff)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes() = %#v, want %#v", got, want)
	}
}

func TestChangesIgnoresDeletedFiles(t *testing.T) {
	diff := strings.NewReader(`diff --git a/lib/deleted.ml b/lib/deleted.ml
deleted file mode 100644
--- a/lib/deleted.ml
+++ /dev/null
@@ -7 +0,0 @@
-deleted
diff --git a/lib/example.mli b/lib/example.mli
--- a/lib/example.mli
+++ b/lib/example.mli
@@ -1 +1 @@
-old
+new
`)

	want := []fileChanges{{
		Path:  "/project/lib/example.mli",
		Lines: []int{1},
	}}
	got, err := changes("/project", diff)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes() = %#v, want %#v", got, want)
	}
}

func TestChangesHandlesRenamedPathsWithSpaces(t *testing.T) {
	diff := strings.NewReader(`diff --git "a/lib/old name.ml" "b/lib/new name.ml"
similarity index 80%
rename from lib/old name.ml
rename to lib/new name.ml
--- "a/lib/old name.ml"
+++ "b/lib/new name.ml"
@@ -1 +1 @@
-old
+new
`)
	want := []fileChanges{{
		Path:  "/project/lib/new name.ml",
		Lines: []int{1},
	}}

	got, err := changes("/project", diff)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes() = %#v, want %#v", got, want)
	}
}

func TestSymbolsContainingLines(t *testing.T) {
	symbols := []Symbol{
		{
			Name:  "first",
			Range: Range{Start: Position{Line: 0}, End: Position{Line: 2}},
		},
		{
			Name:  "second",
			Range: Range{Start: Position{Line: 4}, End: Position{Line: 8}},
		},
	}
	want := []Symbol{symbols[1]}

	got := symbolsContainingLines(symbols, []int{6})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("symbolsContainingLines() = %#v, want %#v", got, want)
	}
}

func TestRangeContainsLineTreatsEndAsExclusive(t *testing.T) {
	symbolRange := Range{
		Start: Position{Line: 2, Character: 4},
		End:   Position{Line: 5, Character: 0},
	}
	if !rangeContainsLine(symbolRange, 4) {
		t.Fatal("rangeContainsLine() rejected the last included line")
	}
	if rangeContainsLine(symbolRange, 5) {
		t.Fatal("rangeContainsLine() included an exclusive end line")
	}
}

func TestQualifyOCamlSymbol(t *testing.T) {
	got := qualifyOCamlSymbol(
		"/project/example.ml",
		[]string{"Nested", "Deeper"},
		"run",
	)
	if want := "Example.Nested.Deeper.run"; got != want {
		t.Fatalf("qualifyOCamlSymbol() = %q, want %q", got, want)
	}
}

func TestRunWithOCamlLSP(t *testing.T) {
	requireOCamlTools(t)
	root := buildOCamlIndex(t, "ocaml_project")

	diff := strings.NewReader(`diff --git a/example.ml b/example.ml
--- a/example.ml
+++ b/example.ml
@@ -2 +2 @@
-  let double value =
+  let double value =
`)
	var output bytes.Buffer
	if err := run(root, diff, &output); err != nil {
		t.Fatal(err)
	}

	result := output.String()
	if !strings.HasPrefix(result, "Places affected by a change in Example.Arithmetic.double\n") {
		t.Fatalf("run() output has an unexpected header:\n%s", result)
	}
	for _, expected := range []string{
		"consumer.ml:4:  Example.Arithmetic.double value",
		"consumer.ml:7:  Arithmetic.double value",
		"consumer.ml:11:  double value",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("run() output does not contain %q:\n%s", expected, result)
		}
	}
}

func TestRunWithComplexOCamlLayouts(t *testing.T) {
	requireOCamlTools(t)
	root := buildOCamlIndex(t, "complex_ocaml_project")

	diff := strings.NewReader(`diff --git a/lib/math.ml b/lib/math.ml
--- a/lib/math.ml
+++ b/lib/math.ml
@@ -3 +3 @@
-    value * 2
+    value * 3
@@ -12 +12 @@
-    Operation.apply value
+    Operation.apply (value + 1)
`)
	var output bytes.Buffer
	if err := run(root, diff, &output); err != nil {
		t.Fatal(err)
	}

	result := output.String()
	if headings := strings.Count(result, "Places affected by a change in "); headings != 2 {
		t.Fatalf("run() produced %d headings, want 2:\n%s", headings, result)
	}
	assertContainsAll(t, result, []string{
		"Places affected by a change in Math.Nested.transform",
		"bin/main.ml:2:  Toolkit.Math.Nested.transform 4",
		"bin/main.ml:5:  Toolkit.Reexport.Nested.transform 5",
		"bin/main.ml:8:  Toolkit.Reexport.Alias.transform 6",
		"lib/consumers.ml:2:  Math.Nested.transform value",
		"lib/consumers.ml:6:  Nested.transform value",
		"lib/consumers.ml:10:  transform value",
		"lib/math.ml:2:  let transform value =",
		"lib/math.ml:16:  Nested.transform value",
		"lib/reexport.ml:6:  Nested.transform value",
		"lib/reexport.ml:9:  Alias.transform value",
		"Places affected by a change in Math.Make.run",
		"bin/main.ml:11:  Toolkit.Functor_ops.Runner.run 7",
		"lib/functor_ops.ml:9:  Runner.run value",
		"lib/math.ml:11:  let run value =",
	})
}

func requireOCamlTools(t *testing.T) {
	t.Helper()
	for _, executable := range []string{"dune", "ocamllsp"} {
		if _, err := exec.LookPath(executable); err != nil {
			t.Fatalf("%s is required for integration tests: %v", executable, err)
		}
	}
}

func buildOCamlIndex(t *testing.T, project string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("testdata", project))
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("dune", "build", "@ocaml-index")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("dune build: %v\n%s", err, output)
	}
	return root
}

func assertContainsAll(t *testing.T, output string, expected []string) {
	t.Helper()
	for _, value := range expected {
		if !strings.Contains(output, value) {
			t.Errorf("output does not contain %q:\n%s", value, output)
		}
	}
}
