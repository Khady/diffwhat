package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGitDiffModesAndRepositoryDiscovery(t *testing.T) {
	root := initGitRepository(t)
	nested := filepath.Join(root, "nested", "directory")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	discovered, err := repositoryRoot(nested)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	canonicalDiscovered, err := filepath.EvalSymlinks(discovered)
	if err != nil {
		t.Fatal(err)
	}
	if canonicalDiscovered != canonicalRoot {
		t.Fatalf("repositoryRoot() = %q, want %q", discovered, root)
	}

	readme := filepath.Join(root, "README.md")
	if err := os.WriteFile(readme, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	unstaged, err := gitDiff(root, false, "")
	if err != nil {
		t.Fatal(err)
	}
	assertChangedLines(t, root, unstaged, []fileChanges{{
		Path:  readme,
		Lines: []int{1},
	}})

	runGit(t, root, "add", "README.md")
	cleanWorkingTree, err := gitDiff(root, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cleanWorkingTree) != 0 {
		t.Fatalf("unstaged diff after git add = %q, want empty", cleanWorkingTree)
	}

	staged, err := gitDiff(root, true, "")
	if err != nil {
		t.Fatal(err)
	}
	assertChangedLines(t, root, staged, []fileChanges{{
		Path:  readme,
		Lines: []int{1},
	}})

	runGit(t, root, "-c", "user.name=Diffwhat", "-c", "user.email=diffwhat@example.invalid",
		"commit", "-q", "-m", "change")
	revision, err := gitDiff(root, false, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	assertChangedLines(t, root, revision, []fileChanges{{
		Path:  readme,
		Lines: []int{1},
	}})
}

func TestPatchMode(t *testing.T) {
	root := initGitRepository(t)
	patch := `diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1 +1 @@
-baseline
+changed
`
	var stdout, stderr bytes.Buffer
	exitCode := realMain(
		[]string{"-C", root, "--patch"},
		strings.NewReader(patch),
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf("realMain() = %d, stderr: %s", exitCode, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("realMain() output = %q, want empty for unsupported file", stdout.String())
	}
}

func TestPatchModeRejectsGitDiffOptions(t *testing.T) {
	var output bytes.Buffer
	_, err := parseCLI([]string{"--patch", "--staged"}, &output)
	if err == nil {
		t.Fatal("parseCLI() accepted --patch with --staged")
	}
}

func initGitRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("baseline\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "-c", "user.name=Diffwhat", "-c", "user.email=diffwhat@example.invalid",
		"commit", "-q", "-m", "baseline")
	return root
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func assertChangedLines(t *testing.T, root string, diff []byte, want []fileChanges) {
	t.Helper()
	got, err := changes(root, bytes.NewReader(diff))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes() = %#v, want %#v", got, want)
	}
}
