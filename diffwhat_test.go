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
	diff := []string{
		"+++ b/lib/example.ml",
		"@@ -7,2 +10,3 @@",
	}
	want := []fileChanges{{
		Path:  "/project/lib/example.ml",
		Lines: []int{10, 11, 12},
	}}

	got := changes("/project", diff, &strings.Builder{})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes() = %#v, want %#v", got, want)
	}
}

func TestChangesMultipleHunksAndFiles(t *testing.T) {
	diff := []string{
		"+++ b/lib/first.ml",
		"@@ -7 +10 @@",
		"@@ -20,2 +30,2 @@",
		"+++ b/README.md",
		"@@ -1 +1 @@",
		"+++ b/lib/second.ml",
		"@@ -3 +4 @@",
	}
	want := []fileChanges{
		{Path: "/project/lib/first.ml", Lines: []int{10, 30, 31}},
		{Path: "/project/lib/second.ml", Lines: []int{4}},
	}

	got := changes("/project", diff, &strings.Builder{})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes() = %#v, want %#v", got, want)
	}
}

func TestChangesIgnoresDeletedAndNonImplementationFiles(t *testing.T) {
	diff := []string{
		"+++ /dev/null",
		"@@ -7 +0,0 @@",
		"+++ b/lib/example.mli",
		"@@ -1 +1 @@",
	}

	got := changes("/project", diff, &strings.Builder{})
	if len(got) != 0 {
		t.Fatalf("changes() = %#v, want no changes", got)
	}
}

func TestParseHunk(t *testing.T) {
	tests := []struct {
		name   string
		hunk   string
		start  int
		length int
		ok     bool
	}{
		{name: "default length", hunk: "@@ -7 +10 @@", start: 10, length: 1, ok: true},
		{name: "explicit length", hunk: "@@ -20,2 +30,2 @@ let example =", start: 30, length: 2, ok: true},
		{name: "empty new range", hunk: "@@ -20 +30,0 @@", start: 30, length: 0, ok: true},
		{name: "invalid", hunk: "@@ invalid @@", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, length, ok := parseHunk(tt.hunk)
			if start != tt.start || length != tt.length || ok != tt.ok {
				t.Fatalf("parseHunk(%q) = (%d, %d, %t), want (%d, %d, %t)",
					tt.hunk, start, length, ok, tt.start, tt.length, tt.ok)
			}
		})
	}
}

func TestFunctionsIncludesNestedModules(t *testing.T) {
	outlines := []outline{
		{Kind: "Value", Name: "top_level", Start: position{Line: 1}, End: position{Line: 2}},
		{
			Kind: "Module",
			Name: "Nested",
			Children: []outline{{
				Kind: "Value", Name: "inner", Start: position{Line: 4}, End: position{Line: 7},
			}},
		},
		{Kind: "Exn", Name: "Ignored", Start: position{Line: 9}, End: position{Line: 10}},
	}
	want := []function{
		{Name: "Example.top_level", Start: 1, End: 2},
		{Name: "Example.Nested.inner", Start: 4, End: 7},
	}

	got := functions(outlines, "/project/example.ml")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("functions() = %#v, want %#v", got, want)
	}
}

func TestParseOutline(t *testing.T) {
	data := []byte(`{"class":"return","value":[{"start":{"line":1,"col":0},"end":{"line":2,"col":3},"name":"run","kind":"Value","children":[]},{"start":{"line":3,"col":0},"end":{"line":3,"col":4},"name":"E","kind":"Exn","children":[]}]}`)

	outlines, err := parseOutline(data)
	if err != nil {
		t.Fatal(err)
	}
	got := functions(outlines, "example.ml")
	want := []function{{Name: "Example.run", Start: 1, End: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("functions() = %#v, want %#v", got, want)
	}
}

func TestRunWithMerlinAndOCPGrep(t *testing.T) {
	for _, executable := range []string{"dune", "ocamlmerlin", "ocp-grep"} {
		if _, err := exec.LookPath(executable); err != nil {
			t.Fatalf("%s is required for integration tests: %v", executable, err)
		}
	}

	root, err := filepath.Abs(filepath.Join("testdata", "ocaml_project"))
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("dune", "build")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("dune build: %v\n%s", err, output)
	}

	diff := strings.NewReader("+++ b/example.ml\n@@ -2 +2 @@\n")
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
