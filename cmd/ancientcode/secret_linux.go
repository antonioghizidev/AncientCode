//go:build linux

// Masked secret input for /connect (M14). Uses the existing raw-mode driver so
// the key is never echoed and never reaches the terminal scrollback.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"
)

// errNotTerminal is returned when there is no terminal to type into. The
// onboarding of D-2026-10-02-67 depends on this being an error rather than a
// blocking read: a dialog on a pipe hangs, and hanging is worse than failing.
var errNotTerminal = errors.New("stdin is not a terminal")

// readSecret reads one line from stdin with echo disabled and returns it.
//
// Why raw mode rather than simply turning ECHO off in canonical mode: the REPL
// already owns a raw line editor, and reusing it keeps one code path for
// backspace, Ctrl+C and Ctrl+D. ECHO is already cleared by enableRawInput, and
// we print our own mask so the operator sees that something was typed.
func readSecret(label string) (string, error) {
	term, err := enableRawInput()
	if err != nil {
		return "", errNotTerminal
	}
	defer term.restore()

	if label != "" {
		fmt.Printf("%s", label)
	}

	var out []rune
	reader := bufio.NewReader(os.Stdin)
	for {
		c, _, err := reader.ReadRune()
		if err != nil {
			fmt.Println()
			return "", err
		}
		switch {
		case c == '\r' || c == '\n':
			fmt.Println()
			return string(out), nil
		case c == 0x7f || c == '\b':
			// Backspace: erase the mask we printed.
			if len(out) > 0 {
				out = out[:len(out)-1]
				fmt.Print("\b \b")
			}
		case c == 0x03:
			// Ctrl+C: ISIG stays enabled, so this raises SIGINT and never
			// reaches us. Kept as a guard for terminals with ISIG disabled.
			fmt.Println()
			return "", errors.New("cancelado")
		case c == 0x04:
			if len(out) == 0 {
				fmt.Println()
				return "", errors.New("cancelado")
			}
		case c == '\t':
			// A key never contains a tab; silently dropping it avoids the
			// operator pasting one from a wrong selection.
		case unicode.IsControl(c):
			// Every other control character is dropped: a pasted ESC sequence
			// must not be able to drive the terminal while a secret is on
			// screen.
		default:
			out = append(out, c)
			fmt.Print("*")
		}
	}
}

// hasStdinTerminal reports whether stdin is a character device. Used to decide
// whether onboarding may open a dialog at all.
func hasStdinTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// trimSecret normalises a pasted key: providers issue keys without surrounding
// whitespace, and a paste often brings a trailing newline.
func trimSecret(s string) string {
	return strings.TrimSpace(s)
}
