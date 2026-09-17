// Interactive line input for the REPL (M8, D-2026-09-16-37). The line
// EDITOR is a pure state machine (echo-free, terminal-free) so ↑/↓ handling,
// backspace and Ctrl+C all have unit tests. The raw TTY DRIVER lives in
// raw_linux.go / raw_other.go and is only active when stdin is a char device
// AND the platform is linux (the Inspiron target + WSL). Pipes, files and
// --run stay byte-stable through the cooked path.
package main

import (
	"bufio"
	"context"
	"io"
	"os"
	"time"
)

// Sentinel runes produced by the raw key decoder for non-printable keys. They
// live outside the printable BMP so they can never collide with user input.
const (
	runeLeft  = rune(0x0900)
	runeRight = rune(0x0901)
	runeUp    = rune(0x0902)
	runeDown  = rune(0x0903)
	runeHome  = rune(0x0904)
	runeEnd   = rune(0x0905)
	runeDel   = rune(0x0906)
	runeCtrlU = rune(0x0015)
	runeCtrlC = rune(0x0003)
	runeCtrlD = rune(0x0004)
)

// lineDoneKind tells how a line completed.
type lineDoneKind int

const (
	lineNone  lineDoneKind = iota // still editing
	lineEnter                     // Enter pressed
	lineCtrlC                     // Ctrl+C (caller decides semantics)
	lineCtrlD                     // soft EOF (empty line)
)

// editor is the pure line buffer used by raw prompts and gate confirmations.
// It holds an optional redacted history for ↑/↓ navigation.
type editor struct {
	buf     []rune
	pos     int
	hist    []string
	histIdx int // -1 = editing a fresh line; >=0 = browsing history
	draft   string
}

func newEditor(hist []string) *editor {
	return &editor{hist: hist, histIdx: -1}
}

// current returns the edited line text.
func (e *editor) current() string { return string(e.buf) }

// handle processes one decoded key and reports the completion kind. A line is
// only handed back on lineEnter/lineCtrlC/lineCtrlD.
func (e *editor) handle(r rune) (string, lineDoneKind) {
	switch r {
	case runeCtrlC:
		return "", lineCtrlC
	case '\r', '\n':
		return string(e.buf), lineEnter
	case runeCtrlD:
		if len(e.buf) == 0 {
			return "", lineCtrlD // soft EOF
		}
		return "", lineNone
	case runeCtrlU:
		e.buf = nil
		e.pos = 0
		return "", lineNone
	case '\b', 0x7f:
		if e.pos > 0 {
			e.pos--
			e.buf = append(e.buf[:e.pos], e.buf[e.pos+1:]...)
		}
		return "", lineNone
	case runeLeft:
		if e.pos > 0 {
			e.pos--
		}
		return "", lineNone
	case runeRight:
		if e.pos < len(e.buf) {
			e.pos++
		}
		return "", lineNone
	case runeHome:
		e.pos = 0
		return "", lineNone
	case runeEnd:
		e.pos = len(e.buf)
		return "", lineNone
	case runeUp:
		e.historyStep(-1)
		return "", lineNone
	case runeDown:
		e.historyStep(1)
		return "", lineNone
	default:
		if r >= 0x20 {
			e.buf = append(e.buf, 0)
			copy(e.buf[e.pos+1:], e.buf[e.pos:])
			e.buf[e.pos] = r
			e.pos++
		}
		return "", lineNone
	}
}

// historyStep moves the cursor in the prompt history, keeping the fresh draft
// when the user goes back down past the newest entry.
func (e *editor) historyStep(delta int) {
	if len(e.hist) == 0 {
		return
	}
	next := e.histIdx + delta
	switch {
	case e.histIdx == -1 && delta > 0:
		// going down from a fresh line: nothing older to browse.
		return
	case e.histIdx == -1 && delta < 0:
		e.draft = string(e.buf)
		e.histIdx = len(e.hist) - 1
	case next < 0:
		e.histIdx = 0 // clamp at the oldest entry
	case next >= len(e.hist):
		e.histIdx = -1
		e.buf = []rune(e.draft)
		e.pos = len(e.buf)
		return
	default:
		e.histIdx = next
	}
	if e.histIdx >= 0 {
		e.buf = []rune(e.hist[e.histIdx])
		e.pos = len(e.buf)
	}
}

// render writes the prompt + current line over the current row, ending with
// the cursor on the insertion point. Errors are ignored: a dead terminal must
// never break the agent loop.
func (e *editor) render(w io.Writer, prompt string) {
	s := "\r" + prompt + string(e.buf) + "\x1b[K"
	l := len([]rune(s))
	moveLeft := l - len([]rune(prompt)) - e.pos
	if moveLeft > 0 {
		s += "\x1b[" + itoa(moveLeft) + "D"
	}
	_, _ = io.WriteString(w, s)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// readKey decodes one key from the terminal: printable runes pass through,
// Ctrl+C/D come as control runes, and arrow/home/end CSI sequences are
// collapsed into sentinels. A lone ESC (no following bytes within a short
// deadline) is ignored.
func readKey(in *bufio.Reader, fd *os.File) rune {
	r, _, err := in.ReadRune()
	if err != nil {
		if err == io.EOF {
			return '\n'
		}
		return 0
	}
	if r != 0x1b {
		return r
	}
	// CSI: ESC [ X (arrows) or ESC [ X ~ (home/end variants).
	_ = fd.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	seq, _, err := in.ReadRune()
	_ = fd.SetReadDeadline(time.Time{})
	if err != nil || seq != '[' {
		return 0 // stray ESC
	}
	k, _, err := in.ReadRune()
	if err != nil {
		return 0
	}
	switch k {
	case 'A':
		return runeUp
	case 'B':
		return runeDown
	case 'C':
		return runeRight
	case 'D':
		return runeLeft
	case 'H':
		return runeHome
	case 'F':
		return runeEnd
	case '1', '4', '7':
		// Home/End on some terminals come as ESC [ 1~ / 4~ / 7~.
		t, _, err := in.ReadRune()
		if err != nil || t != '~' {
			return 0
		}
		if k == '4' {
			return runeEnd
		}
		return runeHome
	}
	return 0
}

// rawReader couples the bufio reader with the raw-mode fd so prompts can
// re-render and the gate can confirm keys.
type rawReader struct {
	in  *bufio.Reader
	fd  *os.File
	tty bool
}

// promptLineRaw drives the editor on a raw terminal: it prints the prompt,
// echoes keystrokes, and returns the completed line and its completion kind
// (Enter / Ctrl+C / Ctrl+D).
func promptLineRaw(rr *rawReader, prompt string, hist []string) (line string, kind lineDoneKind) {
	ed := newEditor(hist)
	ed.render(os.Stdout, prompt)
	for {
		k := readKey(rr.in, rr.fd)
		line, kk := ed.handle(k)
		if kk != lineNone {
			io.WriteString(os.Stdout, "\n")
			return line, kk
		}
		ed.render(os.Stdout, prompt)
	}
}

// confirmRaw reads a short gate answer ([y/N] etc.) on the raw terminal. It
// bails out (interrupted) when the turn context is canceled, so a Ctrl+C or an
// approval timeout inside the gate never leaves the reader blocked.
func confirmRaw(ctx context.Context, rr *rawReader, prompt string) (line string, kind lineDoneKind) {
	ed := newEditor(nil)
	ed.render(os.Stderr, prompt)
	type res struct {
		line string
		kind lineDoneKind
	}
	ch := make(chan res, 1)
	go func() {
		for {
			k := readKey(rr.in, rr.fd)
			line, kk := ed.handle(k)
			if kk != lineNone {
				ch <- res{line: line, kind: kk}
				return
			}
			ed.render(os.Stderr, prompt)
		}
	}()
	select {
	case r := <-ch:
		io.WriteString(os.Stderr, "\n")
		return r.line, r.kind
	case <-ctx.Done():
		io.WriteString(os.Stderr, "\n")
		return "", lineCtrlC
	}
}
