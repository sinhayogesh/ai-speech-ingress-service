// Package mock provides a mock STT adapter for testing without cloud credentials.
package mock

import (
	"context"
	"time"

	"ai-speech-ingress-service/internal/service/stt"
)

// Adapter implements stt.Adapter with mock responses.
type Adapter struct {
	cb stt.Callback
}

// New creates a new mock STT adapter.
func New() *Adapter {
	return &Adapter{}
}

// Start begins a mock transcription session.
func (a *Adapter) Start(ctx context.Context, cb stt.Callback) error {
	a.cb = cb
	return nil
}

// SendAudio simulates receiving audio and triggers mock transcripts.
func (a *Adapter) SendAudio(ctx context.Context, audio []byte) error {
	// Simulate processing delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		a.cb.OnPartial("I want to can")
		
		time.Sleep(100 * time.Millisecond)
		a.cb.OnPartial("I want to cancel my")
		
		time.Sleep(100 * time.Millisecond)
		a.cb.OnFinal("I want to cancel my plan", 0.94)
	}()
	return nil
}

// Close ends the mock session.
func (a *Adapter) Close() error {
	return nil
}

