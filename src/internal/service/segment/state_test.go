package segment

import (
	"testing"
)

func TestLifecycle_InitialState(t *testing.T) {
	lc := NewLifecycle("seg-1")

	if lc.State() != StateOpen {
		t.Errorf("expected StateOpen, got %v", lc.State())
	}
	if lc.SegmentId() != "seg-1" {
		t.Errorf("expected seg-1, got %v", lc.SegmentId())
	}
	if !lc.CanEmitPartial() {
		t.Error("expected CanEmitPartial to be true")
	}
	if !lc.CanEmitFinal() {
		t.Error("expected CanEmitFinal to be true")
	}
	if lc.IsClosed() {
		t.Error("expected IsClosed to be false")
	}
}

func TestLifecycle_EmitPartial_InOpenState(t *testing.T) {
	lc := NewLifecycle("seg-1")

	// Should allow multiple partials
	for i := 0; i < 5; i++ {
		if err := lc.EmitPartial(); err != nil {
			t.Errorf("partial %d: unexpected error: %v", i, err)
		}
	}

	// State should still be OPEN
	if lc.State() != StateOpen {
		t.Errorf("expected StateOpen after partials, got %v", lc.State())
	}
}

func TestLifecycle_EmitFinal_TransitionsToFinalEmitted(t *testing.T) {
	lc := NewLifecycle("seg-1")

	if err := lc.EmitFinal(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if lc.State() != StateFinalEmitted {
		t.Errorf("expected StateFinalEmitted, got %v", lc.State())
	}
	if lc.CanEmitPartial() {
		t.Error("expected CanEmitPartial to be false after final")
	}
	if lc.CanEmitFinal() {
		t.Error("expected CanEmitFinal to be false after final")
	}
}

func TestLifecycle_EmitFinal_OnlyOnce(t *testing.T) {
	lc := NewLifecycle("seg-1")

	// First final should succeed
	if err := lc.EmitFinal(); err != nil {
		t.Errorf("first final: unexpected error: %v", err)
	}

	// Second final should fail
	if err := lc.EmitFinal(); err != ErrFinalAlreadyEmitted {
		t.Errorf("second final: expected ErrFinalAlreadyEmitted, got %v", err)
	}
}

func TestLifecycle_EmitPartial_FailsAfterFinal(t *testing.T) {
	lc := NewLifecycle("seg-1")

	// Emit final first
	if err := lc.EmitFinal(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Partial should fail
	if err := lc.EmitPartial(); err != ErrCannotEmitPartialAfterFinal {
		t.Errorf("expected ErrCannotEmitPartialAfterFinal, got %v", err)
	}
}

func TestLifecycle_Close_TransitionsToClosed(t *testing.T) {
	lc := NewLifecycle("seg-1")

	lc.Close()

	if lc.State() != StateClosed {
		t.Errorf("expected StateClosed, got %v", lc.State())
	}
	if !lc.IsClosed() {
		t.Error("expected IsClosed to be true")
	}
}

func TestLifecycle_Close_Idempotent(t *testing.T) {
	lc := NewLifecycle("seg-1")

	lc.Close()
	lc.Close()
	lc.Close()

	if lc.State() != StateClosed {
		t.Errorf("expected StateClosed, got %v", lc.State())
	}
}

func TestLifecycle_OperationsFailAfterClose(t *testing.T) {
	lc := NewLifecycle("seg-1")
	lc.Close()

	if err := lc.EmitPartial(); err != ErrSegmentClosed {
		t.Errorf("EmitPartial: expected ErrSegmentClosed, got %v", err)
	}

	if err := lc.EmitFinal(); err != ErrSegmentClosed {
		t.Errorf("EmitFinal: expected ErrSegmentClosed, got %v", err)
	}
}

func TestLifecycle_Reset(t *testing.T) {
	lc := NewLifecycle("seg-1")

	// Emit final and close
	lc.EmitFinal()
	lc.Close()

	// Reset to new segment
	lc.Reset("seg-2")

	if lc.SegmentId() != "seg-2" {
		t.Errorf("expected seg-2, got %v", lc.SegmentId())
	}
	if lc.State() != StateOpen {
		t.Errorf("expected StateOpen after reset, got %v", lc.State())
	}
	if !lc.CanEmitPartial() {
		t.Error("expected CanEmitPartial to be true after reset")
	}
	if !lc.CanEmitFinal() {
		t.Error("expected CanEmitFinal to be true after reset")
	}
}

func TestLifecycle_FullCycle(t *testing.T) {
	lc := NewLifecycle("seg-1")

	// Emit partials
	for i := 0; i < 3; i++ {
		if err := lc.EmitPartial(); err != nil {
			t.Fatalf("partial %d failed: %v", i, err)
		}
	}

	// Emit final
	if err := lc.EmitFinal(); err != nil {
		t.Fatalf("final failed: %v", err)
	}

	// Close
	lc.Close()

	// Verify final state
	if lc.State() != StateClosed {
		t.Errorf("expected StateClosed, got %v", lc.State())
	}
}

func TestState_String(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateOpen, "OPEN"},
		{StateFinalEmitted, "FINAL_EMITTED"},
		{StateClosed, "CLOSED"},
		{StateDropped, "DROPPED"},
		{State(99), "UNKNOWN(99)"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("State(%d).String() = %v, want %v", tt.state, got, tt.expected)
		}
	}
}

// --- Tests for DROPPED state (error handling) ---

func TestLifecycle_Drop_FromOpenState(t *testing.T) {
	lc := NewLifecycle("seg-1")

	// Drop should succeed from OPEN
	if !lc.Drop() {
		t.Error("expected Drop() to return true from OPEN state")
	}

	if lc.State() != StateDropped {
		t.Errorf("expected StateDropped, got %v", lc.State())
	}
	if !lc.IsClosed() {
		t.Error("expected IsClosed to be true for dropped segment")
	}
	if !lc.IsDropped() {
		t.Error("expected IsDropped to be true")
	}
}

func TestLifecycle_Drop_FromFinalEmittedState(t *testing.T) {
	lc := NewLifecycle("seg-1")
	lc.EmitFinal()

	// Drop should succeed from FINAL_EMITTED
	if !lc.Drop() {
		t.Error("expected Drop() to return true from FINAL_EMITTED state")
	}

	if lc.State() != StateDropped {
		t.Errorf("expected StateDropped, got %v", lc.State())
	}
}

func TestLifecycle_Drop_Idempotent(t *testing.T) {
	lc := NewLifecycle("seg-1")

	// First drop succeeds
	if !lc.Drop() {
		t.Error("expected first Drop() to return true")
	}

	// Subsequent drops return false (already terminal)
	if lc.Drop() {
		t.Error("expected second Drop() to return false")
	}
	if lc.Drop() {
		t.Error("expected third Drop() to return false")
	}

	if lc.State() != StateDropped {
		t.Errorf("expected StateDropped, got %v", lc.State())
	}
}

func TestLifecycle_Drop_FailsAfterClose(t *testing.T) {
	lc := NewLifecycle("seg-1")
	lc.Close()

	// Drop should fail from CLOSED (already terminal)
	if lc.Drop() {
		t.Error("expected Drop() to return false from CLOSED state")
	}

	// State should remain CLOSED, not DROPPED
	if lc.State() != StateClosed {
		t.Errorf("expected StateClosed, got %v", lc.State())
	}
}

func TestLifecycle_OperationsFailAfterDrop(t *testing.T) {
	lc := NewLifecycle("seg-1")
	lc.Drop()

	// All operations should fail after drop
	if err := lc.EmitPartial(); err != ErrSegmentClosed {
		t.Errorf("EmitPartial: expected ErrSegmentClosed, got %v", err)
	}

	if err := lc.EmitFinal(); err != ErrSegmentClosed {
		t.Errorf("EmitFinal: expected ErrSegmentClosed, got %v", err)
	}
}

func TestLifecycle_Drop_MidUtterance(t *testing.T) {
	// Simulate real production scenario:
	// - Partials are being emitted
	// - STT error occurs
	// - Segment is dropped (no final emitted)
	lc := NewLifecycle("seg-1")

	// Emit some partials
	for i := 0; i < 3; i++ {
		if err := lc.EmitPartial(); err != nil {
			t.Fatalf("partial %d failed: %v", i, err)
		}
	}

	// Error occurs - drop the segment
	if !lc.Drop() {
		t.Error("expected Drop() to succeed mid-utterance")
	}

	// Verify no final can be emitted
	if err := lc.EmitFinal(); err != ErrSegmentClosed {
		t.Errorf("expected ErrSegmentClosed after drop, got %v", err)
	}

	// Verify state
	if lc.State() != StateDropped {
		t.Errorf("expected StateDropped, got %v", lc.State())
	}
	if !lc.IsDropped() {
		t.Error("expected IsDropped to be true")
	}
}

func TestState_IsTerminal(t *testing.T) {
	tests := []struct {
		state      State
		isTerminal bool
	}{
		{StateOpen, false},
		{StateFinalEmitted, false},
		{StateClosed, true},
		{StateDropped, true},
	}

	for _, tt := range tests {
		if got := tt.state.IsTerminal(); got != tt.isTerminal {
			t.Errorf("State(%s).IsTerminal() = %v, want %v", tt.state, got, tt.isTerminal)
		}
	}
}
