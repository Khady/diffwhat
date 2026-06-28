package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
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
	opened map[uri.URI]struct{}
}

func newOCamlBackend(ctx context.Context, root string) (*ocamlBackend, error) {
	client, err := startLSPClient(ctx, root, "ocamllsp")
	if err != nil {
		return nil, err
	}
	if !documentSymbolProviderEnabled(client.capabilities.DocumentSymbolProvider) {
		client.abort()
		return nil, fmt.Errorf("ocamllsp does not advertise document symbol support")
	}
	if !referencesProviderEnabled(client.capabilities.ReferencesProvider) {
		client.abort()
		return nil, fmt.Errorf("ocamllsp does not advertise reference support")
	}
	return &ocamlBackend{client: client, opened: make(map[uri.URI]struct{})}, nil
}

func (b *ocamlBackend) Symbols(ctx context.Context, path string) ([]Symbol, error) {
	uri, err := b.open(path)
	if err != nil {
		return nil, err
	}

	result, err := b.client.server.DocumentSymbol(ctx, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	})
	if err != nil {
		return nil, err
	}
	b.reportMessages()

	switch result := result.(type) {
	case protocol.DocumentSymbolSlice:
		return hierarchicalOCamlSymbols(path, result), nil
	case protocol.SymbolInformationSlice:
		return flatOCamlSymbols(path, result), nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("ocamllsp returned unsupported document symbol result %T", result)
	}
}

func hierarchicalOCamlSymbols(path string, documentSymbols []protocol.DocumentSymbol) []Symbol {
	var symbols []Symbol
	var walk func([]protocol.DocumentSymbol, []string)
	walk = func(nodes []protocol.DocumentSymbol, containers []string) {
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
			if node.Kind == protocol.SymbolKindModule {
				childContainers = append(append([]string(nil), containers...), node.Name)
			}
			walk(node.Children, childContainers)
		}
	}
	walk(documentSymbols, nil)
	return symbols
}

func flatOCamlSymbols(path string, information []protocol.SymbolInformation) []Symbol {
	var symbols []Symbol
	for _, item := range information {
		if !isOCamlValue(item.Kind) {
			continue
		}
		containers := []string(nil)
		if item.ContainerName != nil {
			containers = []string{*item.ContainerName}
		}
		symbols = append(symbols, Symbol{
			Name:           item.Name,
			QualifiedName:  qualifyOCamlSymbol(path, containers, item.Name),
			Kind:           item.Kind,
			Range:          item.Location.Range,
			SelectionRange: item.Location.Range,
		})
	}
	return symbols
}

func (b *ocamlBackend) References(
	ctx context.Context,
	path string,
	position Position,
) ([]Location, error) {
	uri, err := b.open(path)
	if err != nil {
		return nil, err
	}
	locations, err := b.client.server.References(ctx, &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     position,
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if err != nil {
		return nil, err
	}
	b.reportMessages()
	return locations, nil
}

func (b *ocamlBackend) open(path string) (uri.URI, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("make document path absolute: %w", err)
	}
	documentURI := uri.File(absolutePath)
	if _, ok := b.opened[documentURI]; ok {
		return documentURI, nil
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if err := b.client.server.DidOpen(b.client.ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        documentURI,
			LanguageID: protocol.LanguageKind("ocaml"),
			Version:    1,
			Text:       string(source),
		},
	}); err != nil {
		return "", err
	}
	b.opened[documentURI] = struct{}{}
	return documentURI, nil
}

func (b *ocamlBackend) Close() error {
	for uri := range b.opened {
		if err := b.client.server.DidClose(
			b.client.ctx,
			&protocol.DidCloseTextDocumentParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			},
		); err != nil {
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

func isOCamlValue(kind protocol.SymbolKind) bool {
	return kind == protocol.SymbolKindVariable || kind == protocol.SymbolKindFunction
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
