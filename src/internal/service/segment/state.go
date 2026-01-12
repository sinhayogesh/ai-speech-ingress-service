// Package segment provides segment ID generation and lifecycle management.
package segment

import (
	"errors"
	"fmt"
	"sync"
)

// State represents the lifecycle state of a segment.
type State int

const (
	// StateOpen - Segment is active, can emit partials.
	StateOpen State = iota
	// StateFinalEmitted - Final transcript emitted, waiting to close.
	StateFinalEmitted
	// StateClosed - Segment is closed normally.
	StateClosed
	// StateDropped - Segment was dropped due to error (no final emitted).
	// This is a terminal state. "Silence > bad data"
	StateDropped
)

// String returns the string representation of the state.
func (s State) String() string {
	switch s {
	case StateOpen:
		return "OPEN"
	case StateFinalEmitted:
		return "FINAL_EMITTED"
	case StateClosed:
		return "CLOSED"
	case StateDropped:
		return "DROPPED"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", s)
	}
}

// IsTerminal returns true if the state is terminal (CLOSED or DROPPED).
func (s State) IsTerminal() bool {
	return s == StateClosed || s == StateDropped
}

// Errors for invalid state transitions.
var (
	ErrSegmentClosed               = errors.New("segment is closed")
	ErrFinalAlreadyEmitted         = errors.New("final already emitted for this segment")
	ErrCannotEmitPartialAfterFinal = errors.New("cannot emit partial after final")
)

// Lifecycle manages the state machine for a single segment.
// Thread-safe for concurrent access.
//
// State transitions:
//
//	OPEN → FINAL_EMITTED → CLOSED
//	  │         │
//	  │         └── EmitFinal() ──→ only once
//	  │
//	  └── EmitPartial() ──→ multiple times
//
// Rules:
//   - OPEN: Can emit partials (multiple), can emit final (once → transitions to FINAL_EMITTED)
//   - FINAL_EMITTED: Cannot emit partials, cannot emit final again, can close
//   - CLOSED: All operations are no-ops or return errors
type Lifecycle struct {
	mu        sync.RWMutex
	segmentId string
	state     State
}

// NewLifecycle creates a new segment lifecycle in OPEN state.
func NewLifecycle(segmentId string) *Lifecycle {
	return &Lifecycle{
		segmentId: segmentId,
		state:     StateOpen,
	}
}

// SegmentId returns the segment ID.
func (l *Lifecycle) SegmentId() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.segmentId
}

// State returns the current state.
func (l *Lifecycle) State() State {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.state
}

// CanEmitPartial returns true if a partial can be emitted.
func (l *Lifecycle) CanEmitPartial() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.state == StateOpen
}

// CanEmitFinal returns true if a final can be emitted.
func (l *Lifecycle) CanEmitFinal() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.state == StateOpen
}

// IsClosed returns true if the segment is in a terminal state (closed or dropped).
func (l *Lifecycle) IsClosed() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.state.IsTerminal()
}

// IsDropped returns true if the segment was dropped due to error.
func (l *Lifecycle) IsDropped() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.state == StateDropped
}

// EmitPartial validates and records a partial emission.
// Returns nil if allowed, error if not allowed.
func (l *Lifecycle) EmitPartial() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	switch l.state {
	case StateOpen:
		// OK - partials allowed in OPEN state
		return nil
	case StateFinalEmitted:
		return ErrCannotEmitPartialAfterFinal
	case StateClosed, StateDropped:
		return ErrSegmentClosed
	default:
		return fmt.Errorf("unexpected state: %v", l.state)
	}
}

// EmitFinal validates and transitions to FINAL_EMITTED state.
// Returns nil if allowed (and transitions state), error if not allowed.
func (l *Lifecycle) EmitFinal() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	switch l.state {
	case StateOpen:
		// Transition to FINAL_EMITTED
		l.state = StateFinalEmitted
		return nil
	case StateFinalEmitted:
		return ErrFinalAlreadyEmitted
	case StateClosed, StateDropped:
		return ErrSegmentClosed
	default:
		return fmt.Errorf("unexpected state: %v", l.state)
	}
}

// Close transitions the segment to CLOSED state.
// Can be called from any state. Idempotent.
func (l *Lifecycle) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.state = StateClosed
}

// Drop transitions the segment to DROPPED state.
// Use when an error occurs and the segment should be abandoned without emitting a final.
// "Silence > bad data" - it's better to emit nothing than incorrect/incomplete data.
//
// Scenarios:
//   - STT error mid-utterance
//   - gRPC client disconnect
//   - Network hiccups / stream resets
//   - Partial stream that never received a final
//
// Returns true if the segment was dropped, false if already in a terminal state.
func (l *Lifecycle) Drop() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.state.IsTerminal() {
		return false // Already in terminal state
	}
	l.state = StateDropped
	return true
}

// Reset resets the lifecycle to OPEN state with a new segment ID.
// Used when transitioning to a new segment after OnEndOfUtterance.
func (l *Lifecycle) Reset(newSegmentId string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.segmentId = newSegmentId
	l.state = StateOpen
}
