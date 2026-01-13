// Package stt defines the interface for Speech-to-Text adapters.
package stt

import "context"

// Callback receives transcript results from the STT provider.
type Callback interface {
	// OnPartial is called when an interim/partial transcript is received.
	OnPartial(text string)

	// OnFinal is called when a final transcript is received for the current utterance.
	OnFinal(text string, confidence float64)

	// OnEndOfUtterance is called when the STT provider detects the end of an utterance.
	// This signals that the current segment is complete and a new segment should begin
	// for subsequent speech. The handler should:
	// 1. Finalize the current segment
	// 2. Generate a new segmentId
	// 3. Continue processing audio in the new segment
	OnEndOfUtterance()

	// OnError is called when an error occurs during transcription.
	OnError(err error)
}

// Adapter defines the interface for STT providers (Google, Azure, AWS, etc.).
type Adapter interface {
	// Start begins a streaming transcription session.
	Start(ctx context.Context, cb Callback) error

	// SendAudio sends audio bytes to the STT provider.
	SendAudio(ctx context.Context, audio []byte) error

	// Restart closes the current session and starts a new one.
	// Used after OnEndOfUtterance to continue transcribing subsequent speech.
	// For providers like Google with SingleUtterance mode, this is required
	// to transcribe multiple utterances in a single audio stream.
	Restart(ctx context.Context) error

	// Close ends the session and releases resources.
	Close() error
}
