package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type fileChanges struct {
	Path  string
	Lines []int
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
			if ok {
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

func symbolsContainingLines(symbols []Symbol, lines []int) []Symbol {
	changedLines := make(map[int]struct{}, len(lines))
	for _, line := range lines {
		if line > 0 {
			changedLines[line-1] = struct{}{}
		}
	}

	var changed []Symbol
	for _, symbol := range symbols {
		for line := range changedLines {
			if rangeContainsLine(symbol.Range, line) {
				changed = append(changed, symbol)
				break
			}
		}
	}
	return changed
}

func rangeContainsLine(symbolRange Range, line int) bool {
	if line < symbolRange.Start.Line || line > symbolRange.End.Line {
		return false
	}
	return line != symbolRange.End.Line ||
		symbolRange.End.Character > 0 ||
		symbolRange.Start.Line == symbolRange.End.Line
}

type affectedSymbol struct {
	File   string
	Symbol Symbol
}

func run(gitRoot string, input io.Reader, output io.Writer) error {
	return runContext(context.Background(), gitRoot, input, output)
}

func runContext(ctx context.Context, gitRoot string, input io.Reader, output io.Writer) error {
	diff, err := readLines(input)
	if err != nil {
		return fmt.Errorf("read diff: %w", err)
	}
	allChanges := changes(gitRoot, diff, os.Stderr)

	for _, language := range languages {
		var languageChanges []fileChanges
		for _, file := range allChanges {
			if language.Supports(file.Path) {
				languageChanges = append(languageChanges, file)
			}
		}
		if len(languageChanges) == 0 {
			continue
		}

		backend, err := language.Start(ctx, gitRoot)
		if err != nil {
			return fmt.Errorf("start %s backend: %w", language.Name, err)
		}

		err = analyzeLanguage(ctx, backend, languageChanges, output)
		closeErr := backend.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return fmt.Errorf("stop %s backend: %w", language.Name, closeErr)
		}
	}
	return nil
}

func analyzeLanguage(
	ctx context.Context,
	backend LanguageBackend,
	files []fileChanges,
	output io.Writer,
) error {
	changed := make(map[string]affectedSymbol)
	for _, file := range files {
		symbols, err := backend.Symbols(ctx, file.Path)
		if err != nil {
			return fmt.Errorf("analyze %s: %w", file.Path, err)
		}
		for _, symbol := range symbolsContainingLines(symbols, file.Lines) {
			key := fmt.Sprintf(
				"%s:%d:%d",
				file.Path,
				symbol.SelectionRange.Start.Line,
				symbol.SelectionRange.Start.Character,
			)
			changed[key] = affectedSymbol{File: file.Path, Symbol: symbol}
		}
	}

	affected := make([]affectedSymbol, 0, len(changed))
	for _, symbol := range changed {
		affected = append(affected, symbol)
	}
	sort.Slice(affected, func(i, j int) bool {
		if affected[i].Symbol.QualifiedName != affected[j].Symbol.QualifiedName {
			return affected[i].Symbol.QualifiedName < affected[j].Symbol.QualifiedName
		}
		if affected[i].File != affected[j].File {
			return affected[i].File < affected[j].File
		}
		return positionLess(
			affected[i].Symbol.SelectionRange.Start,
			affected[j].Symbol.SelectionRange.Start,
		)
	})

	sourceCache := make(map[string][]string)
	for _, affectedSymbol := range affected {
		symbol := affectedSymbol.Symbol
		references, err := backend.References(ctx, affectedSymbol.File, symbol.SelectionRange.Start)
		if err != nil {
			return fmt.Errorf("find references to %s: %w", symbol.QualifiedName, err)
		}
		references = deduplicateLocations(references)
		sortLocations(references)

		fmt.Fprintf(output, "Places affected by a change in %s\n", symbol.QualifiedName)
		for _, reference := range references {
			rendered, err := renderLocation(reference, sourceCache)
			if err != nil {
				return err
			}
			fmt.Fprintln(output, rendered)
		}
		fmt.Fprintln(output)
	}
	return nil
}

func renderLocation(location Location, sourceCache map[string][]string) (string, error) {
	path, err := pathFromFileURI(location.URI)
	if err != nil {
		return "", err
	}
	lines, ok := sourceCache[path]
	if !ok {
		file, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("read reference source %s: %w", path, err)
		}
		lines, err = readLines(file)
		closeErr := file.Close()
		if err != nil {
			return "", fmt.Errorf("read reference source %s: %w", path, err)
		}
		if closeErr != nil {
			return "", fmt.Errorf("close reference source %s: %w", path, closeErr)
		}
		sourceCache[path] = lines
	}

	line := location.Range.Start.Line
	if line < 0 || line >= len(lines) {
		return "", fmt.Errorf("reference line %d is outside %s", line+1, path)
	}
	return fmt.Sprintf("%s:%d:%s", path, line+1, lines[line]), nil
}

func sortLocations(locations []Location) {
	sort.Slice(locations, func(i, j int) bool {
		if locations[i].URI != locations[j].URI {
			return locations[i].URI < locations[j].URI
		}
		return positionLess(locations[i].Range.Start, locations[j].Range.Start)
	})
}

func deduplicateLocations(locations []Location) []Location {
	seen := make(map[string]struct{}, len(locations))
	result := make([]Location, 0, len(locations))
	for _, location := range locations {
		key := fmt.Sprintf(
			"%s:%d:%d:%d:%d",
			location.URI,
			location.Range.Start.Line,
			location.Range.Start.Character,
			location.Range.End.Line,
			location.Range.End.Character,
		)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, location)
	}
	return result
}

func positionLess(a, b Position) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Character < b.Character
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
