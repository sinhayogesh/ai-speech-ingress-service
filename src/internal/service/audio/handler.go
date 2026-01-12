// Package audio provides the audio stream handler that coordinates
// between the STT adapter and the event publisher.
package audio

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"ai-speech-ingress-service/internal/events"
	"ai-speech-ingress-service/internal/models"
	"ai-speech-ingress-service/internal/service/segment"
	"ai-speech-ingress-service/internal/service/stt"
)

// SegmentLimits defines safety guardrails for segment processing.
// These prevent unbounded resource usage and ensure backpressure.
type SegmentLimits struct {
	MaxAudioBytes int64         // Max buffered audio per segment
	MaxDuration   time.Duration // Max segment duration
	MaxPartials   int           // Max partial transcripts per segment
}

// DefaultLimits returns sensible default limits.
func DefaultLimits() SegmentLimits {
	return SegmentLimits{
		MaxAudioBytes: 5 * 1024 * 1024, // 5MB (~625 seconds at 8kHz 16-bit mono)
		MaxDuration:   5 * time.Minute, // 5 minutes max segment
		MaxPartials:   500,             // 500 partials max per segment
	}
}

// SegmentTransitionCallback is called when an utterance ends and a new segment begins.
// The callback receives the new segmentId.
type SegmentTransitionCallback func(newSegmentId string)

// Handler manages an audio transcription session.
// It implements stt.Callback to receive transcripts and publish events.
// Uses an explicit segment state machine to enforce lifecycle rules.
// Enforces backpressure limits to prevent unbounded resource usage.
type Handler struct {
	adapter           stt.Adapter
	publisher         *events.Publisher
	segmentGen        *segment.Generator
	interactionId     string
	tenantId          string
	lastAudioOffsetMs int64

	// Segment lifecycle state machine
	lifecycle *segment.Lifecycle

	// Backpressure limits
	limits SegmentLimits

	// Current segment metrics (reset on new segment)
	segmentStartTime time.Time
	audioBytes       int64
	partialCount     int

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
	return NewHandlerWithLimits(adapter, publisher, segmentGen, interactionId, tenantId, segmentId, DefaultLimits())
}

// NewHandlerWithLimits creates a new audio handler with custom segment limits.
func NewHandlerWithLimits(
	adapter stt.Adapter,
	publisher *events.Publisher,
	segmentGen *segment.Generator,
	interactionId, tenantId, segmentId string,
	limits SegmentLimits,
) *Handler {
	return &Handler{
		adapter:          adapter,
		publisher:        publisher,
		segmentGen:       segmentGen,
		interactionId:    interactionId,
		tenantId:         tenantId,
		lifecycle:        segment.NewLifecycle(segmentId),
		limits:           limits,
		segmentStartTime: time.Now(),
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
// Returns error if segment limits are exceeded (segment is dropped).
func (h *Handler) SendAudio(ctx context.Context, audio []byte, audioOffsetMs int64) error {
	h.mu.Lock()
	h.lastAudioOffsetMs = audioOffsetMs
	h.audioBytes += int64(len(audio))
	currentBytes := h.audioBytes
	startTime := h.segmentStartTime
	h.mu.Unlock()

	// Check audio bytes limit
	if h.limits.MaxAudioBytes > 0 && currentBytes > h.limits.MaxAudioBytes {
		reason := fmt.Sprintf("max audio bytes exceeded: %d > %d", currentBytes, h.limits.MaxAudioBytes)
		h.DropSegment(reason)
		return fmt.Errorf("segment limit exceeded: %s", reason)
	}

	// Check duration limit
	if h.limits.MaxDuration > 0 && time.Since(startTime) > h.limits.MaxDuration {
		reason := fmt.Sprintf("max duration exceeded: %v > %v", time.Since(startTime), h.limits.MaxDuration)
		h.DropSegment(reason)
		return fmt.Errorf("segment limit exceeded: %s", reason)
	}

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
// Only emits if segment is in OPEN state and within limits.
func (h *Handler) OnPartial(text string) {
	// Validate state transition
	if err := h.lifecycle.EmitPartial(); err != nil {
		log.Printf("OnPartial ignored: segmentId=%s state=%s err=%v",
			h.lifecycle.SegmentId(), h.lifecycle.State(), err)
		return
	}

	// Track and check partial count limit
	h.mu.Lock()
	h.partialCount++
	count := h.partialCount
	h.mu.Unlock()

	if h.limits.MaxPartials > 0 && count > h.limits.MaxPartials {
		reason := fmt.Sprintf("max partials exceeded: %d > %d", count, h.limits.MaxPartials)
		h.DropSegment(reason)
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

	// Generate new segment ID and reset lifecycle + metrics
	h.mu.Lock()
	h.utteranceCount++
	oldAudioBytes := h.audioBytes
	oldPartialCount := h.partialCount
	oldDuration := time.Since(h.segmentStartTime)

	// Reset segment metrics for new segment
	h.audioBytes = 0
	h.partialCount = 0
	h.segmentStartTime = time.Now()

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

	log.Printf("End of utterance: interactionId=%s oldSegment=%s (state=%s) newSegment=%s utterance=#%d metrics=[bytes=%d partials=%d duration=%v]",
		h.interactionId, oldSegmentId, oldState, newSegmentId, h.utteranceCount,
		oldAudioBytes, oldPartialCount, oldDuration.Round(time.Millisecond))

	// Notify server of segment transition if callback is set
	if cb != nil {
		cb(newSegmentId)
	}
}

// OnError is called when an STT error occurs.
// The current segment is DROPPED - no final will be emitted.
// "Silence > bad data" - it's better to emit nothing than incorrect/incomplete data.
//
// This handles:
//   - STT error mid-utterance
//   - Network hiccups / stream resets
//   - Partial stream without final
func (h *Handler) OnError(err error) {
	segmentId := h.lifecycle.SegmentId()
	oldState := h.lifecycle.State()

	// Drop the segment - no final will be emitted
	dropped := h.lifecycle.Drop()

	log.Printf("STT error - segment DROPPED: interactionId=%s segmentId=%s previousState=%s dropped=%v err=%v",
		h.interactionId, segmentId, oldState, dropped, err)
}

// DropSegment explicitly drops the current segment without emitting a final.
// Use when the segment should be abandoned due to external factors
// (e.g., gRPC client disconnect, timeout, validation failure).
//
// Returns true if the segment was dropped, false if already in a terminal state.
func (h *Handler) DropSegment(reason string) bool {
	segmentId := h.lifecycle.SegmentId()
	oldState := h.lifecycle.State()

	dropped := h.lifecycle.Drop()

	log.Printf("Segment DROPPED: interactionId=%s segmentId=%s previousState=%s reason=%s",
		h.interactionId, segmentId, oldState, reason)

	return dropped
}

// IsSegmentDropped returns true if the current segment was dropped.
func (h *Handler) IsSegmentDropped() bool {
	return h.lifecycle.IsDropped()
}

// SegmentMetrics holds current segment usage metrics.
type SegmentMetrics struct {
	AudioBytes   int64
	PartialCount int
	Duration     time.Duration
}

// GetSegmentMetrics returns current segment metrics for observability.
func (h *Handler) GetSegmentMetrics() SegmentMetrics {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return SegmentMetrics{
		AudioBytes:   h.audioBytes,
		PartialCount: h.partialCount,
		Duration:     time.Since(h.segmentStartTime),
	}
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
