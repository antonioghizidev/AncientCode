package main

import (
	"strings"
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/config"
	"github.com/antonioghizidev/ancientcode/internal/loop"
)

// The TUI must NOT accept --check and silently do nothing.
//
// The REPL did exactly that: it asked for consent to spend tokens and then ran
// no probe, so the operator left believing a model was verified when nobody had
// asked the provider anything. A feature that lies about spending money is
// worse than an absent feature, so the TUI has to say where it works.
func TestTUICheckIsRefusedWithAPointerToTheREPL(t *testing.T) {
	m := mkModel(t, loop.New(&switchableCatalogProvider{model: "test/model"}, nil,
		&config.Config{Provider: "openrouter", Model: "test/model", BaseURL: "https://exemplo.invalido/v1", APIKeyEnv: "CHAVE"}, nil, nil))
	cmd := m.openPicker(catalogOpts{agentViable: true, check: true})

	session := m.session
	joined := strings.ToLower(strings.Join(session, "\n"))
	if !strings.Contains(joined, "repl") {
		t.Fatalf("a TUI aceitou --check sem avisar: %v", session)
	}
	if !strings.Contains(joined, "models --check") {
		t.Fatalf("a TUI nao apontou o comando que funciona: %v", session)
	}
	// And it must not be left armed on the picker, or a later render could
	// pretend the flag is being honoured.
	if m.picker == nil {
		t.Fatal("o picker nem deveria ter sido aberto")
	}
	if m.picker.opts.check {
		t.Fatal("--check continua armado no picker depois de recusado")
	}
	_ = cmd
}

// Without --check the TUI stays silent about it: a line on every /models would
// be noise the operator learns to skip.
func TestTUIWithoutCheckDoesNotMentionIt(t *testing.T) {
	m := mkModel(t, loop.New(&switchableCatalogProvider{model: "test/model"}, nil,
		&config.Config{Provider: "openrouter", Model: "test/model", BaseURL: "https://exemplo.invalido/v1", APIKeyEnv: "CHAVE"}, nil, nil))
	m.openPicker(catalogOpts{agentViable: true})

	joined := strings.ToLower(strings.Join(m.session, "\n"))
	if strings.Contains(joined, "--check") {
		t.Fatalf("a TUI mencionou --check sem pedido: %v", m.session)
	}
}
