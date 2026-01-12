// Package google provides a Google Cloud Speech-to-Text adapter.
package google

import (
	"context"
	"io"

	speech "cloud.google.com/go/speech/apiv1"
	speechpb "google.golang.org/genproto/googleapis/cloud/speech/v1"

	"ai-speech-ingress-service/internal/service/stt"
)

// Adapter implements stt.Adapter using Google Cloud Speech-to-Text.
type Adapter struct {
	client *speech.Client
	stream speechpb.Speech_StreamingRecognizeClient
	cb     stt.Callback
}

// New creates a new Google STT adapter.
// Requires GOOGLE_APPLICATION_CREDENTIALS environment variable to be set.
func New(ctx context.Context) (*Adapter, error) {
	c, err := speech.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return &Adapter{client: c}, nil
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
					Encoding:        speechpb.RecognitionConfig_LINEAR16,
					SampleRateHertz: 8000,
					LanguageCode:    "en-US",
				},
				InterimResults:  true,
				SingleUtterance: true, // Enable utterance boundary detection
			},
		},
	})
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
