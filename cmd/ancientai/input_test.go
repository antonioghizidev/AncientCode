package main

import (
	"bytes"
	"testing"
)

func feedLine(t *testing.T, hist []string, keys ...rune) (string, lineDoneKind) {
	t.Helper()
	ed := newEditor(hist)
	var line string
	var kind lineDoneKind
	for _, k := range keys {
		line, kind = ed.handle(k)
		if kind != lineNone {
			break
		}
	}
	if kind == lineNone {
		t.Fatalf("editor never completed a line (keys=%v)", keys)
	}
	return line, kind
}

func TestEditorPrintsAndCompletes(t *testing.T) {
	line, kind := feedLine(t, nil, 'o', 'i', '\n')
	if kind != lineEnter || line != "oi" {
		t.Fatalf("line=%q kind=%v, want oi/enter", line, kind)
	}
}

func TestEditorBackspace(t *testing.T) {
	line, _ := feedLine(t, nil, 'o', 'i', '\b', '\n')
	if line != "o" {
		t.Fatalf("line=%q, want o", line)
	}
}

func TestEditorCtrlCDetected(t *testing.T) {
	line, kind := feedLine(t, nil, 'x', runeCtrlC)
	if kind != lineCtrlC {
		t.Fatalf("Ctrl+C kind = %v, want lineCtrlC", kind)
	}
	if line != "" {
		t.Fatalf("interrupted line = %q, want empty (never sent as prompt)", line)
	}
}

func TestEditorHistoryNavigation(t *testing.T) {
	hist := []string{"primeiro", "segundo", "terceiro"}
	// Fresh draft then navigate the ring.
	ed := newEditor(hist)
	for _, k := range []rune{'n', 'o', 'v', 'o'} {
		_, _ = ed.handle(k)
	}
	ed.handle(runeUp)
	if got := ed.current(); got != "terceiro" {
		t.Fatalf("Up#1 = %q, want terceiro", got)
	}
	ed.handle(runeUp)
	if got := ed.current(); got != "segundo" {
		t.Fatalf("Up#2 = %q, want segundo", got)
	}
	ed.handle(runeUp)
	if got := ed.current(); got != "primeiro" {
		t.Fatalf("Up#3 = %q, want primeiro", got)
	}
	// Clamp: further Up stays at the oldest entry.
	ed.handle(runeUp)
	if got := ed.current(); got != "primeiro" {
		t.Fatalf("Up#4 (clamp) = %q, want primeiro", got)
	}
	// Down returns through history and back to the fresh draft.
	ed.handle(runeDown)
	if got := ed.current(); got != "segundo" {
		t.Fatalf("Down#1 = %q, want segundo", got)
	}
	ed.handle(runeDown)
	if got := ed.current(); got != "terceiro" {
		t.Fatalf("Down#2 = %q, want terceiro", got)
	}
	ed.handle(runeDown)
	if got := ed.current(); got != "novo" {
		t.Fatalf("Down#3 = %q, want draft novo", got)
	}
	// Completing a line returns the browsed text.
	line, kind := ed.handle('\n')
	if kind != lineEnter || line != "novo" {
		t.Fatalf("complete = %q kind=%v, want novo/enter", line, kind)
	}
}

func TestEditorHistoryEmpty(t *testing.T) {
	ed := newEditor(nil)
	ed.handle(runeUp)
	if got := ed.current(); got != "" {
		t.Fatalf("empty-history Up = %q, want empty", got)
	}
}

func TestEditorCtrlUAndMovement(t *testing.T) {
	ed := newEditor(nil)
	for _, k := range []rune{'a', 'b', 'c'} {
		ed.handle(k)
	}
	ed.handle(runeLeft)
	ed.handle(runeLeft)
	ed.handle('X')
	if got := ed.current(); got != "aXbc" {
		t.Fatalf("insert mid = %q, want aXbc", got)
	}
	ed.handle(runeHome)
	ed.handle(runeEnd)
	ed.handle(runeCtrlU)
	if got := ed.current(); got != "" {
		t.Fatalf("Ctrl+U = %q, want empty", got)
	}
}

func TestEditorRenderCursor(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []rune
		want string
	}{
		{"empty", nil, "\r> \x1b[K"},
		{"end", []rune{'a', 'b'}, "\r> ab\x1b[K"},
		{"middle", []rune{'a', 'b', runeLeft}, "\r> ab\x1b[K\x1b[1D"},
		{"home", []rune{'a', 'b', runeHome}, "\r> ab\x1b[K\x1b[2D"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed := newEditor(nil)
			for _, key := range tc.keys {
				ed.handle(key)
			}
			var out bytes.Buffer
			ed.render(&out, "> ")
			if got := out.String(); got != tc.want {
				t.Fatalf("render = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEditorCtrlDSoftEOF(t *testing.T) {
	line, kind := newEditor(nil).handle(runeCtrlD)
	if kind != lineCtrlD || line != "" {
		t.Fatalf("Ctrl+D on empty = %q kind=%v, want soft EOF", line, kind)
	}
}
