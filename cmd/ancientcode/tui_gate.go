package main

import (
	"context"
	"sync"

	"github.com/antonioghizidev/ancientcode/internal/loop"
)

// gateResult is what a Decide call returns internally (it can be threaded
// through a channel without the loop.Approver indirection for tests).
type gateResult struct {
	Decision loop.Decision
	Err      error
}

// gateOpen is a pending approval presented to the operator. The TUI renders it
// and the operator answers with Respond; Veto hard-denies every future call of
// the same tool for the rest of this session.
type gateOpen struct {
	Tool    string
	Summary string

	resp chan loop.Decision
	veto chan struct{}

	g *tuiGate
}

// Respond resolves the pending approval. Non-blocking: an answer after the
// gate already resolved (e.g. a late keypress on an old modal) is dropped.
func (o *gateOpen) Respond(d loop.Decision) {
	select {
	case o.resp <- d:
	default:
	}
}

// Veto denies this call and registers a permanent deny for o.Tool.
func (o *gateOpen) Veto() {
	select {
	case o.veto <- struct{}{}:
	default:
	}
}

// tuiGate is a single-dialog Approver for the TUI frontend. It implements
// loop.Approver: the loop calls Decide from its turn goroutine and blocks
// until the operator answers in the UI. Only one dialog is open at a time —
// a second Decide waits (serialized) until the operator resolves the first.
//
// Fail-closed: if ctx is done (timeout / Ctrl+C cancels the turn), Decide
// returns DecisionDeny with the context error and nothing is ever approved
// implicitly.
type tuiGate struct {
	// slot serializes dialogs: capacity 1, acquired before emitting a dialog.
	slot   chan struct{}
	openCh chan *gateOpen
	// closedCh is notified after a dialog resolves so the UI can drop the modal.
	closedCh chan *gateOpen

	mu     sync.Mutex
	vetoed map[string]bool
}

func newTuiGate() *tuiGate {
	return &tuiGate{
		slot:     make(chan struct{}, 1),
		openCh:   make(chan *gateOpen),
		closedCh: make(chan *gateOpen),
		vetoed:   make(map[string]bool),
	}
}

// opened yields pending approvals to the UI layer.
func (g *tuiGate) opened() <-chan *gateOpen { return g.openCh }

// closed reports dialogs that have been resolved and should be removed from
// the screen.
func (g *tuiGate) closed() <-chan *gateOpen { return g.closedCh }

// Vetoed reports whether tool was permanently denied by the operator.
func (g *tuiGate) Vetoed(tool string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.vetoed[tool]
}

// registerVeto hard-denies every future Decide for tool.
func (g *tuiGate) registerVeto(tool string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.vetoed[tool] = true
}

// Decide implements loop.Approver. It blocks until the operator answers, the
// context is done, or the tool is vetoed. See type doc for the fail-closed
// guarantees and single-dialog serialization.
func (g *tuiGate) Decide(ctx context.Context, tool, summary string) (loop.Decision, error) {
	if g.Vetoed(tool) {
		return loop.DecisionDeny, nil
	}
	// Acquire the single-dialog slot first; a second concurrent Decide waits.
	select {
	case <-ctx.Done():
		return loop.DecisionDeny, ctx.Err()
	case g.slot <- struct{}{}:
	}
	defer func() { <-g.slot }()

	if g.Vetoed(tool) {
		return loop.DecisionDeny, nil
	}

	open := &gateOpen{
		Tool:    tool,
		Summary: summary,
		resp:    make(chan loop.Decision, 1),
		veto:    make(chan struct{}, 1),
		g:       g,
	}
	select {
	case <-ctx.Done():
		return loop.DecisionDeny, ctx.Err()
	case g.openCh <- open:
	}
	defer func() { g.closeNotify(open) }()

	select {
	case <-ctx.Done():
		return loop.DecisionDeny, ctx.Err()
	case <-open.veto:
		g.registerVeto(tool)
		return loop.DecisionDeny, nil
	case d := <-open.resp:
		return d, nil
	}
}

// closeNotify emits the resolve signal (non-blocking; the UI goroutine drains
// it, so one buffer slot covers the single-dialog lifecycle).
func (g *tuiGate) closeNotify(open *gateOpen) {
	select {
	case g.closedCh <- open:
	default:
	}
}
