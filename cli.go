package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type cliOptions struct {
	directory string
	patch     bool
	staged    bool
	revision  string
}

func realMain(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	options, err := parseCLI(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "diffwhat: %v\n", err)
		return 2
	}

	root, err := repositoryRoot(options.directory)
	if err != nil {
		fmt.Fprintf(stderr, "diffwhat: %v\n", err)
		return 1
	}

	diff := stdin
	if !options.patch {
		data, err := gitDiff(root, options.staged, options.revision)
		if err != nil {
			fmt.Fprintf(stderr, "diffwhat: %v\n", err)
			return 1
		}
		diff = bytes.NewReader(data)
	}

	if err := run(root, diff, stdout); err != nil {
		fmt.Fprintf(stderr, "diffwhat: %v\n", err)
		return 1
	}
	return 0
}

func parseCLI(args []string, output io.Writer) (cliOptions, error) {
	var options cliOptions
	flags := flag.NewFlagSet("diffwhat", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&options.directory, "C", ".", "run as if started in this directory")
	flags.BoolVar(&options.patch, "patch", false, "read a unified diff from standard input")
	flags.BoolVar(&options.staged, "staged", false, "analyze staged changes")
	flags.BoolVar(&options.staged, "cached", false, "alias for --staged")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: diffwhat [OPTIONS] [REVISION]")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Analyze unstaged changes by default. REVISION is passed to git diff")
		fmt.Fprintln(output, "and may be a commit or range such as main...HEAD.")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Options:")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return cliOptions{}, err
	}
	if flags.NArg() > 1 {
		flags.Usage()
		return cliOptions{}, fmt.Errorf("expected at most one revision, got %d", flags.NArg())
	}
	if flags.NArg() == 1 {
		options.revision = flags.Arg(0)
	}
	if options.patch && (options.staged || options.revision != "") {
		return cliOptions{}, fmt.Errorf("--patch cannot be combined with --staged or a revision")
	}
	return options, nil
}

func repositoryRoot(directory string) (string, error) {
	command := exec.Command("git", "-C", directory, "rev-parse", "--show-toplevel")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return "", commandError("find Git repository", err, stderr.String())
	}
	return strings.TrimSpace(string(output)), nil
}

func gitDiff(root string, staged bool, revision string) ([]byte, error) {
	args := []string{
		"-C", root,
		"diff",
		"--unified=0",
		"--no-color",
		"--no-ext-diff",
	}
	if staged {
		args = append(args, "--cached")
	}
	if revision != "" {
		args = append(args, revision)
	}

	command := exec.Command("git", args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, commandError("read Git diff", err, stderr.String())
	}
	return output, nil
}

func commandError(action string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w: %s", action, err, stderr)
}
