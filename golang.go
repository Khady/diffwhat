package main

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

var goLanguage = LanguageDefinition{
	Name: "Go",
	Supports: func(path string) bool {
		return strings.HasSuffix(path, ".go")
	},
	Start: func(ctx context.Context, root string) (LanguageBackend, error) {
		return newGoBackend(ctx, root)
	},
}

type goBackend struct {
	client *lspClient
	opened map[uri.URI]struct{}
}

func newGoBackend(ctx context.Context, root string) (*goBackend, error) {
	client, err := startLSPClient(ctx, root, "gopls")
	if err != nil {
		return nil, err
	}
	if !documentSymbolProviderEnabled(client.capabilities.DocumentSymbolProvider) {
		client.abort()
		return nil, fmt.Errorf("gopls does not advertise document symbol support")
	}
	if !referencesProviderEnabled(client.capabilities.ReferencesProvider) {
		client.abort()
		return nil, fmt.Errorf("gopls does not advertise reference support")
	}
	return &goBackend{client: client, opened: make(map[uri.URI]struct{})}, nil
}

func (b *goBackend) Symbols(ctx context.Context, path string) ([]Symbol, error) {
	documentURI, source, err := b.open(path)
	if err != nil {
		return nil, err
	}
	result, err := b.client.server.DocumentSymbol(ctx, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
	})
	if err != nil {
		return nil, err
	}
	b.reportMessages()

	declarations, err := parseGoDeclarations(path, source)
	if err != nil {
		return nil, err
	}
	switch result := result.(type) {
	case protocol.DocumentSymbolSlice:
		return hierarchicalGoSymbols(result, declarations), nil
	case protocol.SymbolInformationSlice:
		return flatGoSymbols(result, declarations), nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("gopls returned unsupported document symbol result %T", result)
	}
}

type goDeclarations struct {
	packageName string
	functions   []goFunction
}

type goFunction struct {
	name      string
	receiver  string
	startLine uint32
	endLine   uint32
}

func parseGoDeclarations(path string, source []byte) (goDeclarations, error) {
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, path, source, 0)
	if err != nil {
		return goDeclarations{}, fmt.Errorf("parse %s: %w", path, err)
	}
	result := goDeclarations{packageName: file.Name.Name}
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			item := goFunction{
				name:      declaration.Name.Name,
				startLine: uint32(files.Position(declaration.Pos()).Line - 1),
				endLine:   uint32(files.Position(declaration.End()).Line - 1),
			}
			if declaration.Recv != nil && len(declaration.Recv.List) > 0 {
				var receiver bytes.Buffer
				receiverType := ast.Unparen(declaration.Recv.List[0].Type)
				if err := format.Node(&receiver, files, receiverType); err != nil {
					return goDeclarations{}, fmt.Errorf(
						"format receiver for %s: %w",
						declaration.Name,
						err,
					)
				}
				item.receiver = receiver.String()
			}
			result.functions = append(result.functions, item)
		case *ast.GenDecl:
			result.functions = append(
				result.functions,
				goInterfaceMethods(files, declaration)...,
			)
		}
	}
	return result, nil
}

func goInterfaceMethods(files *token.FileSet, declaration *ast.GenDecl) []goFunction {
	if declaration.Tok != token.TYPE {
		return nil
	}
	var methods []goFunction
	for _, specification := range declaration.Specs {
		typeSpecification, ok := specification.(*ast.TypeSpec)
		if !ok {
			continue
		}
		interfaceType, ok := typeSpecification.Type.(*ast.InterfaceType)
		if !ok {
			continue
		}
		for _, method := range interfaceType.Methods.List {
			for _, name := range method.Names {
				methods = append(methods, goFunction{
					name:      name.Name,
					receiver:  typeSpecification.Name.Name,
					startLine: uint32(files.Position(method.Pos()).Line - 1),
					endLine:   uint32(files.Position(method.End()).Line - 1),
				})
			}
		}
	}
	return methods
}

func hierarchicalGoSymbols(
	documentSymbols []protocol.DocumentSymbol,
	declarations goDeclarations,
) []Symbol {
	var symbols []Symbol
	var walk func([]protocol.DocumentSymbol)
	walk = func(nodes []protocol.DocumentSymbol) {
		for _, node := range nodes {
			if isGoCallable(node.Kind) {
				symbols = append(symbols, Symbol{
					Name:           node.Name,
					QualifiedName:  qualifyGoSymbol(declarations, node.Name, node.Range),
					Kind:           node.Kind,
					Range:          node.Range,
					SelectionRange: node.SelectionRange,
				})
			}
			walk(node.Children)
		}
	}
	walk(documentSymbols)
	return symbols
}

func flatGoSymbols(
	information []protocol.SymbolInformation,
	declarations goDeclarations,
) []Symbol {
	var symbols []Symbol
	for _, item := range information {
		if !isGoCallable(item.Kind) {
			continue
		}
		symbols = append(symbols, Symbol{
			Name:           item.Name,
			QualifiedName:  qualifyGoSymbol(declarations, item.Name, item.Location.Range),
			Kind:           item.Kind,
			Range:          item.Location.Range,
			SelectionRange: item.Location.Range,
		})
	}
	return symbols
}

func isGoCallable(kind protocol.SymbolKind) bool {
	return kind == protocol.SymbolKindFunction || kind == protocol.SymbolKindMethod
}

func qualifyGoSymbol(declarations goDeclarations, name string, symbolRange Range) string {
	if separator := strings.LastIndex(name, "."); separator >= 0 {
		name = name[separator+1:]
	}
	for _, function := range declarations.functions {
		if function.name != name ||
			symbolRange.Start.Line < function.startLine ||
			symbolRange.Start.Line > function.endLine {
			continue
		}
		if function.receiver == "" {
			return declarations.packageName + "." + name
		}
		receiver := function.receiver
		if strings.HasPrefix(receiver, "*") {
			receiver = "(" + receiver + ")"
		}
		return declarations.packageName + "." + receiver + "." + name
	}
	return declarations.packageName + "." + name
}

func (b *goBackend) References(
	ctx context.Context,
	path string,
	position Position,
) ([]Location, error) {
	documentURI, _, err := b.open(path)
	if err != nil {
		return nil, err
	}
	locations, err := b.client.server.References(ctx, &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
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

func (b *goBackend) open(path string) (uri.URI, []byte, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", nil, fmt.Errorf("make document path absolute: %w", err)
	}
	source, err := os.ReadFile(absolutePath)
	if err != nil {
		return "", nil, fmt.Errorf("read %s: %w", path, err)
	}
	documentURI := uri.File(absolutePath)
	if _, ok := b.opened[documentURI]; ok {
		return documentURI, source, nil
	}
	if err := b.client.server.DidOpen(b.client.ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        documentURI,
			LanguageID: protocol.LanguageKind("go"),
			Version:    1,
			Text:       string(source),
		},
	}); err != nil {
		return "", nil, err
	}
	b.opened[documentURI] = struct{}{}
	return documentURI, source, nil
}

func (b *goBackend) Close() error {
	for documentURI := range b.opened {
		if err := b.client.server.DidClose(
			b.client.ctx,
			&protocol.DidCloseTextDocumentParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: documentURI},
			},
		); err != nil {
			b.client.abort()
			return err
		}
	}
	return b.client.Close()
}

func (b *goBackend) reportMessages() {
	for _, message := range b.client.takeShowMessages() {
		if message == "Loading packages..." || message == "Finished loading packages." {
			continue
		}
		fmt.Fprintf(os.Stderr, "gopls: %s\n", message)
	}
}
