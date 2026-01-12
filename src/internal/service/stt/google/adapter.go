// Package google provides a Google Cloud Speech-to-Text adapter.
package google

import (
	"context"
	"io"

	speech "cloud.google.com/go/speech/apiv1"
	speechpb "cloud.google.com/go/speech/apiv1/speechpb"

	"ai-speech-ingress-service/internal/service/stt"
)

// Config holds Google STT configuration.
type Config struct {
	LanguageCode   string // BCP-47 language code (e.g., "en-US")
	SampleRateHz   int    // Audio sample rate in Hertz
	InterimResults bool   // Enable partial/interim transcripts
	AudioEncoding  string // Audio encoding (LINEAR16, MULAW, FLAC, etc.)
}

// DefaultConfig returns sensible defaults for Google STT.
func DefaultConfig() Config {
	return Config{
		LanguageCode:   "en-US",
		SampleRateHz:   8000,
		InterimResults: true,
		AudioEncoding:  "LINEAR16",
	}
}

// Adapter implements stt.Adapter using Google Cloud Speech-to-Text.
type Adapter struct {
	client *speech.Client
	stream speechpb.Speech_StreamingRecognizeClient
	cb     stt.Callback
	config Config
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
// Configures single utterance mode to detect end-of-utterance boundaries.
func (a *Adapter) Start(ctx context.Context, cb stt.Callback) error {
	stream, err := a.client.StreamingRecognize(ctx)
	if err != nil {
		return err
	}
	a.stream = stream
	a.cb = cb

	// Send streaming config as the first message
	// SingleUtterance mode tells Google to detect when the speaker stops talking
	return stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config: &speechpb.RecognitionConfig{
					Encoding:        parseAudioEncoding(a.config.AudioEncoding),
					SampleRateHertz: int32(a.config.SampleRateHz),
					LanguageCode:    a.config.LanguageCode,
				},
				InterimResults:  a.config.InterimResults,
				SingleUtterance: true, // Enable utterance boundary detection
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
	return a.stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
			AudioContent: audio,
		},
	})
}

// Close ends the streaming session.
func (a *Adapter) Close() error {
	if a.stream != nil {
		return a.stream.CloseSend()
	}
	return nil
}

// Listen receives transcript responses from Google and invokes callbacks.
// Should be called in a separate goroutine after Start().
// Detects END_OF_SINGLE_UTTERANCE events to signal utterance boundaries.
func (a *Adapter) Listen() {
	for {
		resp, err := a.stream.Recv()
		if err == io.EOF {
			// Stream closed normally
			return
		}
		if err != nil {
			a.cb.OnError(err)
			return
		}

		// Check for end-of-utterance event
		// Google sends this when it detects the speaker has stopped talking
		if resp.SpeechEventType == speechpb.StreamingRecognizeResponse_END_OF_SINGLE_UTTERANCE {
			a.cb.OnEndOfUtterance()
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
				a.cb.OnFinal(alt.Transcript, float64(alt.Confidence))
			} else {
				a.cb.OnPartial(alt.Transcript)
			}
		}
	}
}
