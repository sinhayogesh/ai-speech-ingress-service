// Package mock provides a mock STT adapter for testing without cloud credentials.
// It simulates realistic speech-to-text behavior with progressive partial transcripts,
// exactly one final transcript per utterance, and utterance boundary detection.
package mock

import (
	"context"
	"sync"
	"time"

	"ai-speech-ingress-service/internal/service/stt"
)

// SimulatedUtterance represents a mock utterance with progressive transcripts.
type SimulatedUtterance struct {
	Partials   []string // Progressive partial transcripts
	Final      string   // Final transcript text
	Confidence float64  // Confidence score for final
}

// DefaultUtterances provides sample utterances for simulation.
var DefaultUtterances = []SimulatedUtterance{
	{
		Partials:   []string{"I want", "I want to", "I want to cancel"},
		Final:      "I want to cancel my subscription",
		Confidence: 0.94,
	},
	{
		Partials:   []string{"Yes", "Yes please"},
		Final:      "Yes please go ahead",
		Confidence: 0.97,
	},
	{
		Partials:   []string{"Can you", "Can you help", "Can you help me with"},
		Final:      "Can you help me with my account",
		Confidence: 0.91,
	},
	{
		Partials:   []string{"I've been", "I've been waiting", "I've been waiting for"},
		Final:      "I've been waiting for over an hour",
		Confidence: 0.89,
	},
	{
		Partials:   []string{"Thank you"},
		Final:      "Thank you very much",
		Confidence: 0.98,
	},
}

// Adapter implements stt.Adapter with mock responses.
// It simulates realistic STT behavior:
// - Multiple partial transcripts as audio is received
// - Exactly one final transcript when utterance ends
// - End-of-utterance detection after final transcript
type Adapter struct {
	cb                 stt.Callback
	mu                 sync.Mutex
	audioReceived      int                // Count of audio frames received
	utterance          SimulatedUtterance // Current utterance being simulated
	partialIndex       int                // Next partial to send
	finalSent          bool               // Ensures only one final per utterance
	endOfUtteranceSent bool               // Ensures only one end-of-utterance per utterance
	closed             bool
}

// utteranceCounter tracks which utterance to use next (cycles through defaults)
var (
	utteranceCounter int
	counterMu        sync.Mutex
)

// New creates a new mock STT adapter.
func New() *Adapter {
	counterMu.Lock()
	idx := utteranceCounter % len(DefaultUtterances)
	utteranceCounter++
	counterMu.Unlock()

	return &Adapter{
		utterance: DefaultUtterances[idx],
	}
}

// Start begins a mock transcription session.
func (a *Adapter) Start(ctx context.Context, cb stt.Callback) error {
	a.cb = cb
	return nil
}

// SendAudio simulates receiving audio and triggers progressive partial transcripts.
// When all partials are sent, it simulates end-of-utterance detection (like silence detection).
func (a *Adapter) SendAudio(ctx context.Context, audio []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed || a.cb == nil {
		return nil
	}

	a.audioReceived++

	// Send next partial if available (one partial per audio frame)
	if a.partialIndex < len(a.utterance.Partials) {
		partial := a.utterance.Partials[a.partialIndex]
		a.partialIndex++

		// Simulate processing delay
		go func(text string) {
			time.Sleep(50 * time.Millisecond)
			a.mu.Lock()
			if !a.closed && a.cb != nil {
				a.cb.OnPartial(text)
			}
			a.mu.Unlock()
		}(partial)
	} else if !a.finalSent {
		// All partials sent - simulate utterance completion
		// This mimics silence detection triggering end of utterance
		a.finalSent = true
		a.endOfUtteranceSent = true

		go func() {
			time.Sleep(100 * time.Millisecond)
			a.mu.Lock()
			cb := a.cb
			closed := a.closed
			utt := a.utterance
			a.mu.Unlock()

			if !closed && cb != nil {
				// Send final transcript
				cb.OnFinal(utt.Final, utt.Confidence)
				// Signal end of utterance (speaker stopped talking)
				cb.OnEndOfUtterance()
			}
		}()
	}

	return nil
}

// Close ends the mock session.
// If final wasn't sent via SendAudio (stream ended early), send it now.
func (a *Adapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return nil
	}
	a.closed = true

	// If final wasn't sent yet (stream ended before natural utterance end),
	// send final now based on whatever partials we received
	if !a.finalSent && a.cb != nil {
		a.finalSent = true
		go func() {
			time.Sleep(100 * time.Millisecond)
			a.cb.OnFinal(a.utterance.Final, a.utterance.Confidence)
		}()
	}

	return nil
}
