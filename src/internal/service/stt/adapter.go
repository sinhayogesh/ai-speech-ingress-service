// Package stt defines the interface for Speech-to-Text adapters.
package stt

import "context"

// Callback receives transcript results from the STT provider.
type Callback interface {
	// OnPartial is called when an interim/partial transcript is received.
	OnPartial(text string)

	// OnFinal is called when a final transcript is received.
	OnFinal(text string, confidence float64)

	// OnError is called when an error occurs during transcription.
	OnError(err error)
}

// Adapter defines the interface for STT providers (Google, Azure, AWS, etc.).
type Adapter interface {
	// Start begins a streaming transcription session.
	Start(ctx context.Context, cb Callback) error

	// SendAudio sends audio bytes to the STT provider.
	SendAudio(ctx context.Context, audio []byte) error

	// Close ends the session and releases resources.
	Close() error
}
