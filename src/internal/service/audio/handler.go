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
// Uses an explicit segment state machine to enforce lifecycle rules.
type Handler struct {
	adapter           stt.Adapter
	publisher         *events.Publisher
	segmentGen        *segment.Generator
	interactionId     string
	tenantId          string
	lastAudioOffsetMs int64

	// Segment lifecycle state machine
	lifecycle *segment.Lifecycle

	// Segment transition handling
	mu                  sync.RWMutex
	onSegmentTransition SegmentTransitionCallback
	utteranceCount      int
}

// NewHandler creates a new audio handler for a transcription session.
func NewHandler(
	adapter stt.Adapter,
	publisher *events.Publisher,
	segmentGen *segment.Generator,
	interactionId, tenantId, segmentId string,
) *Handler {
	return &Handler{
		adapter:       adapter,
		publisher:     publisher,
		segmentGen:    segmentGen,
		interactionId: interactionId,
		tenantId:      tenantId,
		lifecycle:     segment.NewLifecycle(segmentId),
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

// Close ends the STT session and closes the current segment.
func (h *Handler) Close() error {
	h.lifecycle.Close()
	return h.adapter.Close()
}

// GetSegmentId returns the current segment ID.
func (h *Handler) GetSegmentId() string {
	return h.lifecycle.SegmentId()
}

// GetSegmentState returns the current segment lifecycle state.
func (h *Handler) GetSegmentState() segment.State {
	return h.lifecycle.State()
}

// GetUtteranceCount returns the number of utterances processed.
func (h *Handler) GetUtteranceCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.utteranceCount
}

// --- stt.Callback implementation ---

// OnPartial is called when an interim transcript is received.
// Only emits if segment is in OPEN state.
func (h *Handler) OnPartial(text string) {
	// Validate state transition
	if err := h.lifecycle.EmitPartial(); err != nil {
		log.Printf("OnPartial ignored: segmentId=%s state=%s err=%v",
			h.lifecycle.SegmentId(), h.lifecycle.State(), err)
		return
	}

	ev := models.TranscriptPartial{
		EventType:     "interaction.transcript.partial",
		InteractionID: h.interactionId,
		TenantID:      h.tenantId,
		SegmentID:     h.lifecycle.SegmentId(),
		Text:          text,
		Timestamp:     time.Now().UnixMilli(),
	}
	h.publishPartial(ev)
}

// OnFinal is called when a final transcript is received.
// Only emits once per segment, transitions to FINAL_EMITTED state.
func (h *Handler) OnFinal(text string, confidence float64) {
	// Validate state transition - this also transitions to FINAL_EMITTED
	if err := h.lifecycle.EmitFinal(); err != nil {
		log.Printf("OnFinal ignored: segmentId=%s state=%s err=%v",
			h.lifecycle.SegmentId(), h.lifecycle.State(), err)
		return
	}

	h.mu.RLock()
	audioOffsetMs := h.lastAudioOffsetMs
	h.mu.RUnlock()

	ev := models.TranscriptFinal{
		EventType:     "interaction.transcript.final",
		InteractionID: h.interactionId,
		TenantID:      h.tenantId,
		SegmentID:     h.lifecycle.SegmentId(),
		Text:          text,
		Confidence:    confidence,
		AudioOffsetMs: audioOffsetMs,
		Timestamp:     time.Now().UnixMilli(),
	}
	h.publishFinal(ev)
}

// OnEndOfUtterance is called when the STT provider detects end of speech.
// This signals the boundary between utterances within a conversation.
// The handler closes the current segment and creates a new one.
func (h *Handler) OnEndOfUtterance() {
	oldSegmentId := h.lifecycle.SegmentId()
	oldState := h.lifecycle.State()

	// Close current segment
	h.lifecycle.Close()

	// Generate new segment ID and reset lifecycle
	h.mu.Lock()
	h.utteranceCount++
	var newSegmentId string
	if h.segmentGen != nil {
		newSegmentId = h.segmentGen.Next(h.interactionId)
	} else {
		newSegmentId = oldSegmentId + "-next"
	}
	cb := h.onSegmentTransition
	h.mu.Unlock()

	// Reset lifecycle for new segment
	h.lifecycle.Reset(newSegmentId)

	log.Printf("End of utterance: interactionId=%s oldSegment=%s (state=%s) newSegment=%s utterance=#%d",
		h.interactionId, oldSegmentId, oldState, newSegmentId, h.utteranceCount)

	// Notify server of segment transition if callback is set
	if cb != nil {
		cb(newSegmentId)
	}
}

// OnError is called when an STT error occurs.
func (h *Handler) OnError(err error) {
	log.Printf("STT error: interactionId=%s segmentId=%s state=%s err=%v",
		h.interactionId, h.lifecycle.SegmentId(), h.lifecycle.State(), err)
}

func (h *Handler) publishPartial(ev models.TranscriptPartial) {
	ctx := context.Background()
	if err := h.publisher.PublishPartial(ctx, h.interactionId, ev); err != nil {
		log.Printf("Failed to publish partial: segmentId=%s err=%v", ev.SegmentID, err)
	}
}

func (h *Handler) publishFinal(ev models.TranscriptFinal) {
	ctx := context.Background()
	if err := h.publisher.PublishFinal(ctx, h.interactionId, ev); err != nil {
		log.Printf("Failed to publish final: segmentId=%s err=%v", ev.SegmentID, err)
	}
}
