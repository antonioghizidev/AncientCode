package main

import (
	"path/filepath"

	"github.com/antonioghizidev/ancientcode/internal/redact"
	"github.com/antonioghizidev/ancientcode/internal/workspace"
)

// handleCd implements /cd in the TUI.
//
// The confirmation is confirm-by-retyping rather than a modal dialog: the
// Bubble Tea event loop cannot block on a prompt, and the operations that DO
// need one (SwitchModel, SwitchProvider) go through a tea.Cmd because they
// emit events and would deadlock the loop. /cd touches nothing asynchronous —
// it flips a string in the workspace — so a modal would be machinery for a
// synchronous mutation.
//
// Retyping the same path to confirm is the sudo pattern: it costs the operator
// one line, it needs no new panel, and it cannot be triggered by a stray keypress.
func (m *tuiModel) handleCd(arg string) {
	if m.ws == nil {
		m.appendSession("! erro: workspace indisponivel nesta sessao")
		return
	}
	res := cdCommand(m.ws, arg)
	if res.Err != nil {
		m.pendingCd = ""
		m.appendSession("! erro: " + redact.Redact(res.Message))
		return
	}
	if arg == "" {
		m.pendingCd = ""
		m.appendSession("> " + redact.Redact(res.Message))
		return
	}
	if !res.NeedsConfirm {
		m.pendingCd = ""
		m.applyCd(res)
		return
	}
	// Same path twice confirms; a different one replaces the pending one.
	if m.pendingCd == arg {
		m.pendingCd = ""
		m.applyCd(res)
		return
	}
	m.pendingCd = arg
	m.appendSession("! " + redact.Redact(res.Message))
	m.appendSession("> isso AMPLIA o alcance de escrita. Repita o comando para confirmar: /cd " + arg)
}

func (m *tuiModel) applyCd(res cdResult) {
	out := res.Apply(m.ws, true)
	if out.Err != nil {
		m.appendSession("! erro: " + redact.Redact(out.Err.Error()))
		return
	}
	if !out.Changed {
		m.appendSession("> ja estamos em " + redact.Redact(out.Root))
		return
	}
	if m.persist != nil {
		persistRoot(m.persist, out.Root)
	}
	m.appendSession("> raiz do projeto: " + redact.Redact(out.Root))
}

// projectLine is the always-visible indicator of WHERE the agent works.
//
// The operator asked for this explicitly, and it is not decoration: without it,
// `pwd` in a shell and the project the tools confine to can silently disagree,
// and the agent edits one folder while the operator believes it is another.
func projectLine(w *workspace.Workspace) string {
	if w == nil {
		return ""
	}
	return filepath.Base(w.ProjectRoot())
}
