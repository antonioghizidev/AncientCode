//go:build !linux

// Masked secret input for /connect, non-Linux fallback (M14).
//
// There is no raw driver here, so the terminal keeps its own echo. The harness
// still refuses to pretend otherwise: readSecret reports that masked input is
// unavailable, and the caller must not fall back to echoing the key.
package main

import "errors"

var errNotTerminal = errors.New("masked input is only supported on linux")

func readSecret(label string) (string, error) {
	return "", errNotTerminal
}

func hasStdinTerminal() bool { return false }

func trimSecret(s string) string { return s }
