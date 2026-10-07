package main

import (
	"context"
	"testing"
	"time"

	"github.com/antonioghizidev/ancientcode/internal/loop"
)

// tuiGate is the Approver dialog (spec 09 §4): it must only ever answer what
// the operator presses, and a canceled context must fail the gate closed
// (nothing executes). These tests drive the gate asynchronously, the same way
// the loop calls it from a turn goroutine.

func TestGateDecideApproveOnYes(t *testing.T) {
	g := newTuiGate()
	done := make(chan gateResult, 1)
	go func() { d, e := g.Decide(context.Background(), "shell", "shell id -u"); done <- gateResult{d, e} }()
	// Wait until the gate is open, then approve like the UI would.
	open := <-g.opened()
	if open.Tool != "shell" {
		t.Fatalf("open tool = %q", open.Tool)
	}
	open.Respond(loop.DecisionApprove)
	select {
	case r := <-done:
		if r.Decision != loop.DecisionApprove || r.Err != nil {
			t.Fatalf("r = %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("gate did not resolve after approve")
	}
	if g.Vetoed("shell") {
		t.Error("approve must not veto")
	}
}

func TestGateDecideDenyOnNo(t *testing.T) {
	g := newTuiGate()
	done := make(chan gateResult, 1)
	go func() { d, e := g.Decide(context.Background(), "shell", "shell id"); done <- gateResult{d, e} }()
	open := <-g.opened()
	open.Respond(loop.DecisionDeny)
	r := <-done
	if r.Decision != loop.DecisionDeny {
		t.Fatalf("deny decision = %v", r.Decision)
	}
}

func TestGateVetoRegistersHardDeny(t *testing.T) {
	g := newTuiGate()
	done := make(chan gateResult, 1)
	go func() {
		d, e := g.Decide(context.Background(), "write_file", "write_file /etc/x")
		done <- gateResult{d, e}
	}()
	open := <-g.opened()
	open.Respond(loop.DecisionDeny) // "d = negar sempre"
	g.registerVeto("write_file")
	r := <-done
	if r.Decision != loop.DecisionDeny {
		t.Fatalf("veto decision = %v", r.Decision)
	}
	if !g.Vetoed("write_file") {
		t.Fatal("veto not registered")
	}
	// A second call for the same tool must be refused instantly, before any
	// dialog opens (the loop also checks Vetoed on allow paths).
	select {
	case <-g.opened():
		t.Fatal("vetoed tool must not open a dialog")
	default:
	}
	d2, e2 := g.Decide(context.Background(), "write_file", "write_file /etc/x")
	if d2 != loop.DecisionDeny {
		t.Fatalf("vetoed Decide = %v", d2)
	}
	if e2 != nil {
		t.Fatalf("vetoed Decide returned err = %v; want immediate deny without error", e2)
	}
}

func TestGateCancelFailsClosed(t *testing.T) {
	g := newTuiGate()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	done := make(chan gateResult, 1)
	go func() { d, e := g.Decide(ctx, "apt", "apt install x"); done <- gateResult{d, e} }()
	open := <-g.opened()
	_ = open // operator never answers; deadline elapses
	// The gate must report deny + a deadline error (loop records a timeout/or
	// denied audit entry) and nothing executes.
	r := <-done
	if r.Decision != loop.DecisionDeny {
		t.Fatalf("cancel decision = %v", r.Decision)
	}
	if r.Err == nil {
		t.Fatal("canceled gate must return an error (fail-closed)")
	}
	// Slot freed: a fresh ask must open a dialog again (UI not stuck).
	g2done := make(chan gateResult, 1)
	go func() { d, e := g.Decide(context.Background(), "shell", "shell id"); g2done <- gateResult{d, e} }()
	select {
	case <-g.opened():
	case <-time.After(2 * time.Second):
		t.Fatal("gate slot not freed after cancel")
	}
}

func TestGateNoDialogsConcurrent(t *testing.T) {
	// The gate is a single dialog: two Decide calls while one is open must not
	// create two modals; the second waits until the first resolves.
	g := newTuiGate()
	d1 := make(chan gateResult, 1)
	d2 := make(chan gateResult, 1)
	go func() { d, e := g.Decide(context.Background(), "shell", "a"); d1 <- gateResult{d, e} }()
	time.Sleep(10 * time.Millisecond) // let the first acquire the slot
	go func() { d, e := g.Decide(context.Background(), "apt", "b"); d2 <- gateResult{d, e} }()
	first := <-g.opened()
	// Second decide must not open its own dialog before the first resolves.
	select {
	case extra := <-g.opened():
		// It is legal for the tool string to match the first (slot serializes);
		// but a *distinct* dialog here would mean two modals at once. With a
		// single slot the second Decide cannot proceed, so this channel is
		// either empty or carries the SAME dialog pointer (first).
		if extra != first {
			t.Fatalf("second dialog opened concurrently: %p != %p", extra, first)
		}
	case <-time.After(60 * time.Millisecond):
	}
	first.Respond(loop.DecisionApprove)
	if r := <-d1; r.Decision != loop.DecisionApprove {
		t.Fatalf("first decision = %v", r.Decision)
	}
	second := <-g.opened()
	second.Respond(loop.DecisionDeny)
	if r := <-d2; r.Decision != loop.DecisionDeny {
		t.Fatalf("second decision = %v", r.Decision)
	}
}
