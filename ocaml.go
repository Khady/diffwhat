package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	lspSymbolModule   = 2
	lspSymbolFunction = 12
	lspSymbolVariable = 13
)

var ocamlLanguage = LanguageDefinition{
	Name: "OCaml",
	Supports: func(path string) bool {
		return strings.HasSuffix(path, ".ml")
	},
	Start: func(ctx context.Context, root string) (LanguageBackend, error) {
		return newOCamlBackend(ctx, root)
	},
}

type ocamlBackend struct {
	client *lspClient
	opened map[string]struct{}
}

type documentSymbol struct {
	Name           string           `json:"name"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []documentSymbol `json:"children"`
}

type symbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName"`
}

func newOCamlBackend(ctx context.Context, root string) (*ocamlBackend, error) {
	client, err := startLSPClient(ctx, root, "ocamllsp")
	if err != nil {
		return nil, err
	}
	if !capabilityEnabled(client.capabilities.DocumentSymbolProvider) {
		client.abort()
		return nil, fmt.Errorf("ocamllsp does not advertise document symbol support")
	}
	if !capabilityEnabled(client.capabilities.ReferencesProvider) {
		client.abort()
		return nil, fmt.Errorf("ocamllsp does not advertise reference support")
	}
	return &ocamlBackend{client: client, opened: make(map[string]struct{})}, nil
}

func (b *ocamlBackend) Symbols(_ context.Context, path string) ([]Symbol, error) {
	uri, err := b.open(path)
	if err != nil {
		return nil, err
	}

	var raw []json.RawMessage
	if err := b.client.request("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]string{"uri": uri},
	}, &raw); err != nil {
		return nil, err
	}
	b.reportMessages()
	if len(raw) == 0 {
		return nil, nil
	}

	var shape map[string]json.RawMessage
	if err := json.Unmarshal(raw[0], &shape); err != nil {
		return nil, fmt.Errorf("decode document symbol shape: %w", err)
	}
	if _, flat := shape["location"]; flat {
		return b.flattenSymbolInformation(path, raw)
	}

	var documentSymbols []documentSymbol
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode document symbols: %w", err)
	}
	if err := json.Unmarshal(data, &documentSymbols); err != nil {
		return nil, fmt.Errorf("decode document symbols: %w", err)
	}

	var symbols []Symbol
	var walk func([]documentSymbol, []string)
	walk = func(nodes []documentSymbol, containers []string) {
		for _, node := range nodes {
			if isOCamlValue(node.Kind) {
				symbols = append(symbols, Symbol{
					Name:           node.Name,
					QualifiedName:  qualifyOCamlSymbol(path, containers, node.Name),
					Kind:           node.Kind,
					Range:          node.Range,
					SelectionRange: node.SelectionRange,
				})
			}
			childContainers := containers
			if node.Kind == lspSymbolModule {
				childContainers = append(append([]string(nil), containers...), node.Name)
			}
			walk(node.Children, childContainers)
		}
	}
	walk(documentSymbols, nil)
	return symbols, nil
}

func (b *ocamlBackend) flattenSymbolInformation(
	path string,
	raw []json.RawMessage,
) ([]Symbol, error) {
	var symbols []Symbol
	for _, item := range raw {
		var information symbolInformation
		if err := json.Unmarshal(item, &information); err != nil {
			return nil, fmt.Errorf("decode symbol information: %w", err)
		}
		if !isOCamlValue(information.Kind) {
			continue
		}
		containers := []string(nil)
		if information.ContainerName != "" {
			containers = []string{information.ContainerName}
		}
		symbols = append(symbols, Symbol{
			Name:           information.Name,
			QualifiedName:  qualifyOCamlSymbol(path, containers, information.Name),
			Kind:           information.Kind,
			Range:          information.Location.Range,
			SelectionRange: information.Location.Range,
		})
	}
	return symbols, nil
}

func (b *ocamlBackend) References(
	_ context.Context,
	path string,
	position Position,
) ([]Location, error) {
	uri, err := b.open(path)
	if err != nil {
		return nil, err
	}
	var locations []Location
	if err := b.client.request("textDocument/references", map[string]any{
		"textDocument": map[string]string{"uri": uri},
		"position":     position,
		"context": map[string]bool{
			"includeDeclaration": true,
		},
	}, &locations); err != nil {
		return nil, err
	}
	b.reportMessages()
	return locations, nil
}

func (b *ocamlBackend) open(path string) (string, error) {
	uri, err := fileURI(path)
	if err != nil {
		return "", err
	}
	if _, ok := b.opened[uri]; ok {
		return uri, nil
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if err := b.client.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "ocaml",
			"version":    1,
			"text":       string(source),
		},
	}); err != nil {
		return "", err
	}
	b.opened[uri] = struct{}{}
	return uri, nil
}

func (b *ocamlBackend) Close() error {
	for uri := range b.opened {
		if err := b.client.notify("textDocument/didClose", map[string]any{
			"textDocument": map[string]string{"uri": uri},
		}); err != nil {
			b.client.abort()
			return err
		}
	}
	return b.client.Close()
}

func (b *ocamlBackend) reportMessages() {
	for _, message := range b.client.takeShowMessages() {
		if strings.Contains(message, "Unable to find 'ocamlformat-rpc' binary") {
			continue
		}
		fmt.Fprintf(os.Stderr, "ocamllsp: %s\n", message)
	}
}

func isOCamlValue(kind int) bool {
	return kind == lspSymbolVariable || kind == lspSymbolFunction
}

func qualifyOCamlSymbol(path string, containers []string, name string) string {
	fileModule := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if fileModule != "" {
		fileModule = strings.ToUpper(fileModule[:1]) + fileModule[1:]
	}
	parts := make([]string, 0, len(containers)+2)
	parts = append(parts, fileModule)
	parts = append(parts, containers...)
	parts = append(parts, name)
	return strings.Join(parts, ".")
}
