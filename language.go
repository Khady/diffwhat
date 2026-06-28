package main

import "context"

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Symbol struct {
	Name           string
	QualifiedName  string
	Kind           int
	Range          Range
	SelectionRange Range
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
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

var languages = []LanguageDefinition{ocamlLanguage}
