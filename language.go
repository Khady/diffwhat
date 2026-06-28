package main

import (
	"context"

	"go.lsp.dev/protocol"
)

type Position = protocol.Position
type Range = protocol.Range
type Location = protocol.Location

type Symbol struct {
	Name           string
	QualifiedName  string
	Kind           protocol.SymbolKind
	Range          Range
	SelectionRange Range
}

type LanguageBackend interface {
	Symbols(context.Context, string) ([]Symbol, error)
	References(context.Context, string, Position) ([]Location, error)
	Close() error
}

type LanguageDefinition struct {
	Name     string
	Supports func(string) bool
	Start    func(context.Context, string) (LanguageBackend, error)
}

var languages = []LanguageDefinition{ocamlLanguage, goLanguage}
