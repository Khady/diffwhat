package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

type fileChanges struct {
	Path  string
	Lines []int
}

func changes(gitRoot string, diff io.Reader) ([]fileChanges, error) {
	files, _, err := gitdiff.Parse(diff)
	if err != nil {
		return nil, fmt.Errorf("parse diff: %w", err)
	}

	var result []fileChanges
	for _, file := range files {
		if file.IsDelete || file.NewName == "" || file.IsBinary {
			continue
		}
		changed := fileChanges{
			Path: filepath.Join(gitRoot, filepath.FromSlash(file.NewName)),
		}
		for _, fragment := range file.TextFragments {
			for offset := int64(0); offset < fragment.NewLines; offset++ {
				changed.Lines = append(changed.Lines, int(fragment.NewPosition+offset))
			}
		}
		if len(changed.Lines) > 0 {
			result = append(result, changed)
		}
	}
	return result, nil
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
	if line < 0 {
		return false
	}
	lspLine := uint32(line)
	if lspLine < symbolRange.Start.Line || lspLine > symbolRange.End.Line {
		return false
	}
	return lspLine != symbolRange.End.Line ||
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
	allChanges, err := changes(gitRoot, input)
	if err != nil {
		return err
	}

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
	if !location.URI.IsFile() {
		return "", fmt.Errorf("unsupported reference URI %q", location.URI)
	}
	path := location.URI.FsPath()
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

	line := int(location.Range.Start.Line)
	if line >= len(lines) {
		return "", fmt.Errorf("reference line %d is outside %s", line+1, path)
	}
	return fmt.Sprintf("%s:%d:%s", path, line+1, lines[line]), nil
}

func sortLocations(locations []Location) {
	sort.Slice(locations, func(i, j int) bool {
		if locations[i].URI != locations[j].URI {
			return locations[i].URI.String() < locations[j].URI.String()
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
			location.URI.String(),
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
