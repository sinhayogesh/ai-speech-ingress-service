// Package audio provides the audio stream handler that coordinates
// between the STT adapter and the event publisher.
package audio

import (
	"context"
	"log"
	"sync"
	"time"

	"ai-speech-ingress-service/internal/events"
	"ai-speech-ingress-service/internal/models"
	"ai-speech-ingress-service/internal/service/segment"
	"ai-speech-ingress-service/internal/service/stt"
)

// SegmentTransitionCallback is called when an utterance ends and a new segment begins.
// The callback receives the new segmentId.
type SegmentTransitionCallback func(newSegmentId string)

// Handler manages an audio transcription session.
// It implements stt.Callback to receive transcripts and publish events.
// Supports utterance boundary detection and segment transitions.
type Handler struct {
	adapter           stt.Adapter
	publisher         *events.Publisher
	segments          *segment.Generator
	interactionId     string
	tenantId          string
	segmentId         string
	lastAudioOffsetMs int64

	// Segment transition handling
	mu                  sync.RWMutex
	onSegmentTransition SegmentTransitionCallback
	utteranceCount      int
}

// NewHandler creates a new audio handler for a transcription session.
func NewHandler(
	adapter stt.Adapter,
	publisher *events.Publisher,
	segments *segment.Generator,
	interactionId, tenantId, segmentId string,
) *Handler {
	return &Handler{
		adapter:       adapter,
		publisher:     publisher,
		segments:      segments,
		interactionId: interactionId,
		tenantId:      tenantId,
		segmentId:     segmentId,
	}
}

// SetSegmentTransitionCallback sets a callback for when utterance boundaries are detected.
// This allows the server to handle segment transitions (e.g., create new STT session).
func (h *Handler) SetSegmentTransitionCallback(cb SegmentTransitionCallback) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onSegmentTransition = cb
}

// Start begins the STT session with this handler as the callback receiver.
func (h *Handler) Start(ctx context.Context) error {
	return h.adapter.Start(ctx, h)
}

// SendAudio forwards audio bytes to the STT adapter.
func (h *Handler) SendAudio(ctx context.Context, audio []byte, audioOffsetMs int64) error {
	h.mu.Lock()
	h.lastAudioOffsetMs = audioOffsetMs
	h.mu.Unlock()
	return h.adapter.SendAudio(ctx, audio)
}

// Close ends the STT session.
func (h *Handler) Close() error {
	return h.adapter.Close()
}

// GetSegmentId returns the current segment ID.
func (h *Handler) GetSegmentId() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.segmentId
}

// GetUtteranceCount returns the number of utterances processed.
func (h *Handler) GetUtteranceCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.utteranceCount
}

// --- stt.Callback implementation ---

// OnPartial is called when an interim transcript is received.
func (h *Handler) OnPartial(text string) {
	h.mu.RLock()
	segmentId := h.segmentId
	h.mu.RUnlock()

	ev := models.TranscriptPartial{
		EventType:     "interaction.transcript.partial",
		InteractionID: h.interactionId,
		TenantID:      h.tenantId,
		SegmentID:     segmentId,
		Text:          text,
		Timestamp:     time.Now().UnixMilli(),
	}
	h.publishPartial(ev)
}

// OnFinal is called when a final transcript is received.
func (h *Handler) OnFinal(text string, confidence float64) {
	h.mu.Lock()
	segmentId := h.segmentId
	audioOffsetMs := h.lastAudioOffsetMs
	h.mu.Unlock()

	ev := models.TranscriptFinal{
		EventType:     "interaction.transcript.final",
		InteractionID: h.interactionId,
		TenantID:      h.tenantId,
		SegmentID:     segmentId,
		Text:          text,
		Confidence:    confidence,
		AudioOffsetMs: audioOffsetMs,
		Timestamp:     time.Now().UnixMilli(),
	}
	h.publishFinal(ev)
}

// OnEndOfUtterance is called when the STT provider detects end of speech.
// This signals the boundary between utterances within a conversation.
// The handler transitions to a new segment for subsequent speech.
func (h *Handler) OnEndOfUtterance() {
	h.mu.Lock()
	oldSegmentId := h.segmentId
	h.utteranceCount++

	// Generate new segment ID for the next utterance
	if h.segments != nil {
		h.segmentId = h.segments.Next(h.interactionId)
	}
	newSegmentId := h.segmentId
	cb := h.onSegmentTransition
	h.mu.Unlock()

	log.Printf("End of utterance detected: interactionId=%s oldSegment=%s newSegment=%s utterance=#%d",
		h.interactionId, oldSegmentId, newSegmentId, h.utteranceCount)

	// Notify server of segment transition if callback is set
	if cb != nil {
		cb(newSegmentId)
	}
}

// OnError is called when an STT error occurs.
func (h *Handler) OnError(err error) {
	h.mu.RLock()
	segmentId := h.segmentId
	h.mu.RUnlock()
	log.Printf("STT error: interactionId=%s segmentId=%s err=%v", h.interactionId, segmentId, err)
}

func (h *Handler) publishPartial(ev models.TranscriptPartial) {
	ctx := context.Background()
	h.publisher.PublishPartial(ctx, h.interactionId, ev)
}

func (h *Handler) publishFinal(ev models.TranscriptFinal) {
	ctx := context.Background()
	h.publisher.PublishFinal(ctx, h.interactionId, ev)
}
