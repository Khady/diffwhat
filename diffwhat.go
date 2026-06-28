package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type position struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

type outline struct {
	Start    position  `json:"start"`
	End      position  `json:"end"`
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	Children []outline `json:"children"`
}

type merlinResponse struct {
	Class string    `json:"class"`
	Value []outline `json:"value"`
}

type function struct {
	Name  string
	Start int
	End   int
}

type fileChanges struct {
	Path  string
	Lines []int
}

func parseOutline(data []byte) ([]outline, error) {
	var response merlinResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode Merlin outline: %w", err)
	}
	if response.Class != "return" {
		return nil, fmt.Errorf("Merlin outline returned class %q", response.Class)
	}
	return response.Value, nil
}

func merlinOutline(file string) ([]outline, error) {
	input, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", file, err)
	}
	defer input.Close()

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(
		"ocamlmerlin",
		"single", "outline",
		"-protocol", "json",
		"-verbosity", "0",
		"-filename", file,
	)
	cmd.Stdin = input
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = filepath.Dir(file)
	if err := cmd.Run(); err != nil {
		return nil, commandError("ocamlmerlin", err, stderr.String())
	}

	return parseOutline(stdout.Bytes())
}

func commandError(name string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("%s: %w", name, err)
	}
	return fmt.Errorf("%s: %w: %s", name, err, stderr)
}

func moduleOfFile(file string) string {
	base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	if base == "" {
		return ""
	}
	return strings.ToUpper(base[:1]) + base[1:]
}

func functions(outlines []outline, file string) []function {
	var result []function
	var walk func([]outline, []string)
	walk = func(nodes []outline, modulePath []string) {
		for _, node := range nodes {
			switch node.Kind {
			case "Value":
				path := append(append([]string(nil), modulePath...), node.Name)
				result = append(result, function{
					Name:  strings.Join(path, "."),
					Start: node.Start.Line,
					End:   node.End.Line,
				})
			case "Module":
				path := append(append([]string(nil), modulePath...), node.Name)
				walk(node.Children, path)
			}
		}
	}
	walk(outlines, []string{moduleOfFile(file)})
	return result
}

func ocpGrep(gitRoot, functionPath string) ([]string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("ocp-grep", functionPath)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = gitRoot
	if err := cmd.Run(); err != nil {
		return nil, commandError("ocp-grep", err, stderr.String())
	}

	return readLines(&stdout)
}

func parseHunk(line string) (start, length int, ok bool) {
	plus := strings.IndexByte(line, '+')
	if plus == -1 {
		return 0, 0, false
	}
	rangeEnd := strings.IndexByte(line[plus:], ' ')
	if rangeEnd == -1 {
		rangeEnd = len(line)
	} else {
		rangeEnd += plus
	}

	parts := strings.Split(line[plus+1:rangeEnd], ",")
	if len(parts) < 1 || len(parts) > 2 {
		return 0, 0, false
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	length = 1
	if len(parts) == 2 {
		length, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, false
		}
	}
	return start, length, start >= 0 && length >= 0
}

func changes(gitRoot string, diff []string, warnings io.Writer) []fileChanges {
	var result []fileChanges
	var current *fileChanges

	flush := func() {
		if current != nil {
			result = append(result, *current)
			current = nil
		}
	}

	for _, line := range diff {
		if strings.HasPrefix(line, "+++ ") {
			flush()
			path, ok := diffPath(line)
			if ok && strings.HasSuffix(path, ".ml") {
				current = &fileChanges{Path: filepath.Join(gitRoot, filepath.FromSlash(path))}
			}
			continue
		}
		if current == nil || !strings.HasPrefix(line, "@@") {
			continue
		}

		start, length, ok := parseHunk(line)
		if !ok {
			fmt.Fprintf(warnings, "unable to read hunk line for file %s: %s\n", current.Path, line)
			continue
		}
		for lineNumber := start; lineNumber < start+length; lineNumber++ {
			current.Lines = append(current.Lines, lineNumber)
		}
	}
	flush()
	return result
}

func diffPath(header string) (string, bool) {
	path := strings.TrimPrefix(header, "+++ ")
	if path == "/dev/null" {
		return "", false
	}
	if strings.HasPrefix(path, "b/") {
		path = strings.TrimPrefix(path, "b/")
	} else {
		return "", false
	}
	if path == "" {
		return "", false
	}
	return path, true
}

func changedFunctionsOfFile(file string, lines []int) (map[string]struct{}, error) {
	outlines, err := merlinOutline(file)
	if err != nil {
		return nil, err
	}

	changedLines := make(map[int]struct{}, len(lines))
	for _, line := range lines {
		changedLines[line] = struct{}{}
	}

	changed := make(map[string]struct{})
	for _, fn := range functions(outlines, file) {
		for line := fn.Start; line <= fn.End; line++ {
			if _, ok := changedLines[line]; ok {
				changed[fn.Name] = struct{}{}
				break
			}
		}
	}
	return changed, nil
}

func run(gitRoot string, input io.Reader, output io.Writer) error {
	diff, err := readLines(input)
	if err != nil {
		return fmt.Errorf("read diff: %w", err)
	}

	changed := make(map[string]struct{})
	for _, file := range changes(gitRoot, diff, os.Stderr) {
		names, err := changedFunctionsOfFile(file.Path, file.Lines)
		if err != nil {
			return fmt.Errorf("analyze %s: %w", file.Path, err)
		}
		for name := range names {
			changed[name] = struct{}{}
		}
	}

	names := make([]string, 0, len(changed))
	for name := range changed {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		occurrences, err := ocpGrep(gitRoot, name)
		if err != nil {
			return fmt.Errorf("find references to %s: %w", name, err)
		}
		fmt.Fprintf(output, "Places affected by a change in %s\n", name)
		for _, occurrence := range occurrences {
			fmt.Fprintln(output, occurrence)
		}
		fmt.Fprintln(output)
	}
	return nil
}

func readLines(reader io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}
