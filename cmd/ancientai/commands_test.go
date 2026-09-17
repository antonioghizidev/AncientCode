package main

import "testing"

func TestParseCommandPlainPrompt(t *testing.T) {
	c, ok := parseCommand("  me diga o que e um agente  ")
	if ok || !c.isPrompt() {
		t.Fatalf("plain text parsed as command: %+v ok=%v", c, ok)
	}
}

func TestParseCommandSlashCommands(t *testing.T) {
	for line, want := range map[string]cmdKind{
		"/exit":      cmdExit,
		"/quit":      cmdExit,
		"/reset":     cmdReset,
		"/help":      cmdHelp,
		"/historico": cmdHistory,
		"/history":   cmdHistory,
		"/retry":     cmdRetry,
		"/continue":  cmdContinue,
		"/context":   cmdContext,
	} {
		c, ok := parseCommand(line)
		if !ok || c.kind != want {
			t.Errorf("parseCommand(%q) = %+v ok=%v, want kind=%v", line, c, ok, want)
		}
	}
}

func TestParseCommandModelAndProvider(t *testing.T) {
	c, ok := parseCommand("/model  nvidia/nemotron-3.5-lightning ")
	if !ok || c.kind != cmdModel || c.arg != "nvidia/nemotron-3.5-lightning" {
		t.Fatalf("/model = %+v ok=%v", c, ok)
	}
	c, ok = parseCommand("/model")
	if !ok || c.kind != cmdModel || c.arg != "" {
		t.Fatalf("/model no-arg = %+v ok=%v (arg must be empty = show)", c, ok)
	}
	c, ok = parseCommand("/provider nvidia")
	if !ok || c.kind != cmdProvider || c.arg != "nvidia" {
		t.Fatalf("/provider = %+v ok=%v", c, ok)
	}
}

func TestParseCommandResend(t *testing.T) {
	c, ok := parseCommand("!2")
	if !ok || c.kind != cmdResend || c.index != 2 {
		t.Fatalf("!2 = %+v ok=%v", c, ok)
	}
	// Non-positive and non-numeric indexes are refused (index=-1 sentinel).
	c, _ = parseCommand("!0")
	if c.kind != cmdResend || c.index != -1 {
		t.Fatalf("!0 index = %d, want -1 (refused)", c.index)
	}
	c, _ = parseCommand("!abc")
	if c.kind != cmdResend || c.index != -1 {
		t.Fatalf("!abc index = %d, want -1 (refused)", c.index)
	}
}

func TestParseCommandUnknownSlash(t *testing.T) {
	// An unknown slash command must NOT be treated as a prompt (fail-closed):
	// it maps to /help so the caller shows the vocabulary and refuses.
	c, ok := parseCommand("/sudo rm -rf /")
	if !ok || c.kind != cmdHelp {
		t.Fatalf("unknown command = %+v ok=%v, want help-kind", c, ok)
	}
}
