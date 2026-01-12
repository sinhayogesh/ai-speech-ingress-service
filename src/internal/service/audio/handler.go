// Package audio provides the audio stream handler that coordinates
// between the STT adapter and the event publisher.
package audio

import (
	"context"
	"log"
	"time"

	"ai-speech-ingress-service/internal/events"
	"ai-speech-ingress-service/internal/models"
	"ai-speech-ingress-service/internal/service/stt"
)

// Handler manages an audio transcription session.
// It implements stt.Callback to receive transcripts and publish events.
type Handler struct {
	adapter           stt.Adapter
	publisher         *events.Publisher
	interactionId     string
	tenantId          string
	segmentId         string
	lastAudioOffsetMs int64
}

// NewHandler creates a new audio handler for a transcription session.
func NewHandler(adapter stt.Adapter, publisher *events.Publisher, interactionId, tenantId, segmentId string) *Handler {
	return &Handler{
		adapter:       adapter,
		publisher:     publisher,
		interactionId: interactionId,
		tenantId:      tenantId,
		segmentId:     segmentId,
	}
}

// Start begins the STT session with this handler as the callback receiver.
func (h *Handler) Start(ctx context.Context) error {
	return h.adapter.Start(ctx, h)
}

// SendAudio forwards audio bytes to the STT adapter.
func (h *Handler) SendAudio(ctx context.Context, audio []byte, audioOffsetMs int64) error {
	h.lastAudioOffsetMs = audioOffsetMs
	return h.adapter.SendAudio(ctx, audio)
}

// Close ends the STT session.
func (h *Handler) Close() error {
	return h.adapter.Close()
}

// --- stt.Callback implementation ---

// OnPartial is called when an interim transcript is received.
func (h *Handler) OnPartial(text string) {
	ev := models.TranscriptPartial{
		EventType:     "interaction.transcript.partial",
		InteractionID: h.interactionId,
		TenantID:      h.tenantId,
		SegmentID:     h.segmentId,
		Text:          text,
		Timestamp:     time.Now().UnixMilli(),
	}
	h.publishPartial(ev)
}

// OnFinal is called when a final transcript is received.
func (h *Handler) OnFinal(text string, confidence float64) {
	ev := models.TranscriptFinal{
		EventType:     "interaction.transcript.final",
		InteractionID: h.interactionId,
		TenantID:      h.tenantId,
		SegmentID:     h.segmentId,
		Text:          text,
		Confidence:    confidence,
		AudioOffsetMs: h.lastAudioOffsetMs,
		Timestamp:     time.Now().UnixMilli(),
	}
	h.publishFinal(ev)
}

// OnError is called when an STT error occurs.
func (h *Handler) OnError(err error) {
	log.Printf("STT error: interactionId=%s segmentId=%s err=%v", h.interactionId, h.segmentId, err)
}

func (h *Handler) publishPartial(ev models.TranscriptPartial) {
	ctx := context.Background()
	h.publisher.Publish(ctx, "interaction.transcript.partial", h.interactionId, ev)
}

func (h *Handler) publishFinal(ev models.TranscriptFinal) {
	ctx := context.Background()
	h.publisher.Publish(ctx, "interaction.transcript.final", h.interactionId, ev)
}
