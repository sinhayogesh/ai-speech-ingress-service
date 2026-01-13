// Package google provides a Google Cloud Speech-to-Text adapter.
package google

import (
	"context"
	"io"
	"sync"

	speech "cloud.google.com/go/speech/apiv1"
	speechpb "cloud.google.com/go/speech/apiv1/speechpb"

	"ai-speech-ingress-service/internal/service/stt"
)

// Config holds Google STT configuration.
type Config struct {
	LanguageCode    string // BCP-47 language code (e.g., "en-US")
	SampleRateHz    int    // Audio sample rate in Hertz
	InterimResults  bool   // Enable partial/interim transcripts
	AudioEncoding   string // Audio encoding (LINEAR16, MULAW, FLAC, etc.)
	SingleUtterance bool   // Enable single utterance mode (stops after each utterance)
}

// DefaultConfig returns sensible defaults for Google STT.
func DefaultConfig() Config {
	return Config{
		LanguageCode:    "en-US",
		SampleRateHz:    8000,
		InterimResults:  true,
		AudioEncoding:   "LINEAR16",
		SingleUtterance: true, // Enable by default for utterance boundary detection
	}
}

// Adapter implements stt.Adapter using Google Cloud Speech-to-Text.
type Adapter struct {
	client *speech.Client
	stream speechpb.Speech_StreamingRecognizeClient
	cb     stt.Callback
	config Config
	ctx    context.Context // Store context for Listen goroutine cancellation
	mu     sync.RWMutex    // Protects stream access during restart
}

// New creates a new Google STT adapter with default configuration.
// Requires GOOGLE_APPLICATION_CREDENTIALS environment variable to be set.
func New(ctx context.Context) (*Adapter, error) {
	return NewWithConfig(ctx, DefaultConfig())
}

// NewWithConfig creates a new Google STT adapter with custom configuration.
// Requires GOOGLE_APPLICATION_CREDENTIALS environment variable to be set.
func NewWithConfig(ctx context.Context, cfg Config) (*Adapter, error) {
	c, err := speech.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return &Adapter{client: c, config: cfg}, nil
}

// Start begins a streaming recognition session and sends the initial config.
func (a *Adapter) Start(ctx context.Context, cb stt.Callback) error {
	stream, err := a.client.StreamingRecognize(ctx)
	if err != nil {
		return err
	}
	a.stream = stream
	a.cb = cb
	a.ctx = ctx // Store for graceful shutdown in Listen

	// Send streaming config as the first message
	// SingleUtterance mode (if enabled) tells Google to detect when the speaker stops talking
	return stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config: &speechpb.RecognitionConfig{
					Encoding:        parseAudioEncoding(a.config.AudioEncoding),
					SampleRateHertz: int32(a.config.SampleRateHz),
					LanguageCode:    a.config.LanguageCode,
				},
				InterimResults:  a.config.InterimResults,
				SingleUtterance: a.config.SingleUtterance,
			},
		},
	})
}

// parseAudioEncoding converts string encoding to protobuf enum.
func parseAudioEncoding(encoding string) speechpb.RecognitionConfig_AudioEncoding {
	switch encoding {
	case "LINEAR16":
		return speechpb.RecognitionConfig_LINEAR16
	case "MULAW":
		return speechpb.RecognitionConfig_MULAW
	case "FLAC":
		return speechpb.RecognitionConfig_FLAC
	case "AMR":
		return speechpb.RecognitionConfig_AMR
	case "AMR_WB":
		return speechpb.RecognitionConfig_AMR_WB
	case "OGG_OPUS":
		return speechpb.RecognitionConfig_OGG_OPUS
	case "SPEEX_WITH_HEADER_BYTE":
		return speechpb.RecognitionConfig_SPEEX_WITH_HEADER_BYTE
	case "WEBM_OPUS":
		return speechpb.RecognitionConfig_WEBM_OPUS
	default:
		return speechpb.RecognitionConfig_LINEAR16 // Safe default
	}
}

// SendAudio sends audio bytes to Google Speech-to-Text.
func (a *Adapter) SendAudio(ctx context.Context, audio []byte) error {
	a.mu.RLock()
	stream := a.stream
	a.mu.RUnlock()

	if stream == nil {
		return nil // Stream not ready, skip
	}
	return stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
			AudioContent: audio,
		},
	})
}

// Restart closes the current stream and creates a new one for the next utterance.
// This is required for Google's SingleUtterance mode, which stops accepting audio
// after detecting end-of-utterance. The callback is preserved from Start().
func (a *Adapter) Restart(ctx context.Context) error {
	a.mu.Lock()
	oldStream := a.stream

	// Create a new stream first (before closing old one to minimize gap)
	stream, err := a.client.StreamingRecognize(ctx)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	a.stream = stream
	a.ctx = ctx
	a.mu.Unlock()

	// Close the old stream (CloseSend signals server we're done sending)
	// The old Listen goroutine will exit when it gets EOF or error
	if oldStream != nil {
		oldStream.CloseSend()
	}

	// Send streaming config for the new session
	err = stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config: &speechpb.RecognitionConfig{
					Encoding:        parseAudioEncoding(a.config.AudioEncoding),
					SampleRateHertz: int32(a.config.SampleRateHz),
					LanguageCode:    a.config.LanguageCode,
				},
				InterimResults:  a.config.InterimResults,
				SingleUtterance: a.config.SingleUtterance,
			},
		},
	})
	if err != nil {
		return err
	}

	// Start listening on the new stream
	go a.Listen()

	return nil
}

// Close ends the streaming session and releases resources.
func (a *Adapter) Close() error {
	a.mu.Lock()
	stream := a.stream
	a.stream = nil
	a.mu.Unlock()

	var streamErr error
	if stream != nil {
		streamErr = stream.CloseSend()
	}
	// Close the underlying client to release resources
	if a.client != nil {
		if err := a.client.Close(); err != nil {
			return err
		}
	}
	return streamErr
}

// Listen receives transcript responses from Google and invokes callbacks.
// Should be called in a separate goroutine after Start().
// Detects END_OF_SINGLE_UTTERANCE events to signal utterance boundaries.
// Respects context cancellation for graceful shutdown.
func (a *Adapter) Listen() {
	// Capture stream pointer at start - this goroutine will only work with this stream
	// even if Restart() creates a new stream and starts a new Listen goroutine
	a.mu.RLock()
	stream := a.stream
	ctx := a.ctx
	cb := a.cb
	a.mu.RUnlock()

	if stream == nil || cb == nil {
		return
	}

	for {
		// Check for context cancellation before blocking on Recv
		if ctx != nil && ctx.Err() != nil {
			return
		}

		resp, err := stream.Recv()
		if err == io.EOF {
			// Stream closed normally
			return
		}
		if err != nil {
			// Don't report error if context was cancelled (graceful shutdown)
			if ctx != nil && ctx.Err() != nil {
				return
			}
			// Also check if this is still the current stream
			// If Restart() has already created a new stream, don't report error
			a.mu.RLock()
			currentStream := a.stream
			a.mu.RUnlock()
			if currentStream != stream {
				return // This stream was replaced, exit silently
			}
			cb.OnError(err)
			return
		}

		// Check if this stream has been replaced by Restart()
		// If so, exit - the new Listen goroutine will handle new responses
		a.mu.RLock()
		currentStream := a.stream
		a.mu.RUnlock()
		if currentStream != stream {
			return // This stream was replaced, exit silently
		}

		// Check for end-of-utterance event
		// Google sends this when it detects the speaker has stopped talking
		if resp.SpeechEventType == speechpb.StreamingRecognizeResponse_END_OF_SINGLE_UTTERANCE {
			cb.OnEndOfUtterance()
			// Note: After END_OF_SINGLE_UTTERANCE, Google will still send final results
			// but won't accept more audio. The handler should start a new session.
		}

		// Process transcript results
		for _, r := range resp.Results {
			if len(r.Alternatives) == 0 {
				continue
			}
			alt := r.Alternatives[0]
			if r.IsFinal {
				cb.OnFinal(alt.Transcript, float64(alt.Confidence))
			} else {
				cb.OnPartial(alt.Transcript)
			}
		}
	}
}
