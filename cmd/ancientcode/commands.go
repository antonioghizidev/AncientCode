// Command parsing for the REPL (M8, D-2026-09-16-37). This is a PURE module:
// it only classifies a line into an intent — it never touches the loop, the
// store or any I/O. Every frontend (REPL today, TUI tomorrow, web later)
// shares this same parser so prompt↔UI parity is a build-time property.
package main

import (
	"strconv"
	"strings"
)

// cmdKind enumerates every slash-command and shortcut the REPL understands.
type cmdKind int

const (
	cmdNone cmdKind = iota // plain text: a normal prompt
	cmdExit
	cmdReset
	cmdHelp
	cmdHistory // /historico
	cmdResend  // !N
	cmdRetry   // /retry
	cmdContinue
	cmdModel    // /model [nome|N]
	cmdProvider // /provider [nome]
	cmdContext  // /context
	cmdModels   // /models [--free|--all|--check|texto]
	cmdConnect  // /connect [provedor] [--use-env]
)

// connectOpts are the /connect flags (M14, D-2026-10-02-66).
type connectOpts struct {
	useEnv bool
}

// catalogOpts are the /models filters (M12, D-2026-10-01-53).
type catalogOpts struct {
	free           bool
	includeNonChat bool
	check          bool
	query          string
	// agentViable is ON by default: the catalog hides models this harness cannot
	// drive (no tool calling disclosed, or a context too small for one turn).
	//
	// Measured 2026-10-02 on OpenRouter: 395 of 469 models qualify, so this is a
	// small filter — ordering and paging carry the usability, not this.
	// `/models --viable` turns it off and shows everything the provider lists.
	agentViable bool
	// page is 1-based. -1 means "next" and -2 means "prev", resolved by the
	// caller against the page it last rendered; the parser cannot know it.
	page int
	// pageSet records that the operator explicitly asked to page, so
	// /models <texto> starts back at page 1 instead of sticking wherever it was.
	pageSet bool
}

// command is the parsed form of one REPL line.
type command struct {
	kind    cmdKind
	arg     string // /model X, /provider Y, /connect [provedor]
	index   int    // !N → 1-based prompt index
	opts    catalogOpts
	connOpt connectOpts
}

// parseCommand interprets a single trimmed REPL line. Plain text returns
// (command{}, false); commands return (command{kind: ...}, true). Parsing is
// strict: an unknown slash-command is still a command (cmdNone not used for it
// only for unambiguous dispatch below) — see /parse unknown in tests.
func parseCommand(line string) (command, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return command{}, false
	}
	head, rest, _ := strings.Cut(line, " ")
	rest = strings.TrimSpace(rest)

	switch strings.ToLower(head) {
	case "/exit", "/quit":
		return command{kind: cmdExit}, true
	case "/reset":
		return command{kind: cmdReset}, true
	case "/help":
		return command{kind: cmdHelp}, true
	case "/historico", "/history":
		return command{kind: cmdHistory}, true
	case "/retry":
		return command{kind: cmdRetry}, true
	case "/continue", "/continuar":
		return command{kind: cmdContinue}, true
	case "/context":
		return command{kind: cmdContext}, true
	case "/model":
		return command{kind: cmdModel, arg: rest}, true
	case "/models":
		return command{kind: cmdModels, opts: parseCatalogOpts(rest)}, true
	case "/provider":
		return command{kind: cmdProvider, arg: rest}, true
	case "/connect":
		return command{kind: cmdConnect, arg: rest, connOpt: parseConnectOpts(rest)}, true
	}

	// !N re-enqueues the N-th newest prompt (1 = most recent). Numbers that do
	// not parse or are non-positive are refused (fail-closed); the caller still
	// validates against the actual history depth.
	if strings.HasPrefix(line, "!") {
		n, err := strconv.Atoi(strings.TrimPrefix(line, "!"))
		if err != nil || n <= 0 {
			return command{kind: cmdResend, arg: line, index: -1}, true
		}
		return command{kind: cmdResend, index: n}, true
	}

	// Any other slash-prefixed token is an unknown command. It is refused by
	// the caller; this classifies it so the REPL can give one clear message.
	if strings.HasPrefix(line, "/") {
		return command{kind: cmdHelp, arg: line}, true
	}

	return command{}, false
}

// parseConnectOpts reads /connect flags. Only --use-env exists; an unknown
// token is left in arg so the caller reports it rather than silently treating a
// typo as "connect to this provider".
func parseConnectOpts(rest string) connectOpts {
	var o connectOpts
	for _, tok := range strings.Fields(rest) {
		if strings.EqualFold(tok, "--use-env") {
			o.useEnv = true
		}
	}
	return o
}

// parseCatalogOpts reads /models flags. Unrecognized tokens are treated as the
// search query, so "/models llama" just searches — a filter can never fail
// silently into an empty list because of a typo.
func parseCatalogOpts(rest string) catalogOpts {
	var o catalogOpts
	for _, tok := range strings.Fields(rest) {
		switch strings.ToLower(tok) {
		case "--free":
			o.free = true
		case "--all":
			o.includeNonChat = true
		case "--viable":
			o.agentViable = false
		case "--check":
			o.check = true
		case "--next":
			// -1 and -2 are resolved against the current page by the caller;
			// the parser has no idea what page the operator is on.
			o.page, o.pageSet = -1, true
		case "--prev":
			o.page, o.pageSet = -2, true
		default:
			if o.query == "" {
				o.query = tok
			} else {
				o.query += " " + tok
			}
		}
	}
	return o
}

// isPrompt reports whether a parsed line should be treated as a normal prompt
// (no command semantics).
func (c command) isPrompt() bool { return c.kind == cmdNone }

// helpText lists the REPL commands (shared by /help and banner).
const helpText = "comandos: /exit /quit /reset /help /historico (!N repete) /retry /continue " +
	"/connect [provedor] [--use-env] /model [nome|N] /models [--free|--all|--check|texto] " +
	"/provider [nome] /context | no plano: y aprova e executa, feedback:<msg> replaneja"
