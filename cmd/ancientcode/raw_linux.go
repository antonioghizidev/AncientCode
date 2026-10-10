//go:build linux

// Raw terminal driver for the REPL (M8): switches stdin to raw mode so the
// line editor can process ↑/↓, Ctrl+C etc. Only active on Linux (the Inspiron
// server and WSL) and only when stdin is a char device.
package main

import (
	"errors"
	"syscall"
	"unsafe"
)

// rawTerm stores the file and the previous termios for restoration.
type rawTerm struct {
	fd  int
	old syscall.Termios
}

// enableRawInput puts stdin in raw mode: canonical processing, echo, line
// discipline editing (IEXTEN) and flow control are off, so arrow keys arrive
// as raw escape sequences and backspace as a byte. ISIG stays ON: Ctrl+C must
// raise SIGINT so the main-loop signal handler can cancel the in-flight turn
// (or exit at the prompt) — it is never a byte the editor must handle.
func enableRawInput() (*rawTerm, error) {
	term, ok := stdinTermios()
	if !ok {
		return nil, errors.New("stdin is not a terminal")
	}
	raw := term
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0

	if err := ioctlTermios(syscall.Stdin, syscall.TCSETS, &raw); err != nil {
		return nil, err
	}
	return &rawTerm{fd: syscall.Stdin, old: term}, nil
}

// restore puts the terminal back to its previous mode. It is idempotent-safe
// and errors are ignored: the process is usually exiting anyway.
func (t *rawTerm) restore() {
	if t == nil {
		return
	}
	_ = ioctlTermios(t.fd, syscall.TCSETS, &t.old)
}

func stdinTermios() (syscall.Termios, bool) {
	return termiosFd(uintptr(syscall.Stdin))
}

// termiosFd is the isatty probe behind stdinTermios. It takes the fd as an
// argument so tests can point it at /dev/null — the exact file that made the
// stat-based check lie — without swapping fd 0 under a running test binary.
func termiosFd(fd uintptr) (syscall.Termios, bool) {
	var t syscall.Termios
	if err := ioctlTermios(int(fd), syscall.TCGETS, &t); err != nil {
		return t, false
	}
	return t, true
}

func ioctlTermios(fd int, req uintptr, t *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(t)))
	if errno != 0 {
		return errno
	}
	return nil
}
