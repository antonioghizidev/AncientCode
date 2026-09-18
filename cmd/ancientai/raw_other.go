//go:build !linux

// Cooked fallback for platforms without the raw driver (Windows host dev).
// The REPL stays fully functional — only ↑/↓ editing and Ctrl+C-as-byte are
// unavailable; pipes and --run are unaffected.
package main

import "errors"

type rawTerm struct{}

func enableRawInput() (*rawTerm, error) {
	return nil, errors.New("raw input requires a linux terminal (cooked fallback active)")
}

func (t *rawTerm) restore() {}
