// Package audio provides the audio stream handler that coordinates
// between the STT adapter and the event publisher.
package audio

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"ai-speech-ingress-service/internal/events"
	"ai-speech-ingress-service/internal/models"
	"ai-speech-ingress-service/internal/observability/metrics"
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

	// Stream context for adapter restart
	ctx context.Context

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

	// Utterance boundary tracking
	// When END_OF_SINGLE_UTTERANCE is received, we need to wait for the final
	// before restarting, otherwise we'll have two Listen goroutines racing.
	pendingRestart bool

	// Continuous mode creates new segments after each final (no OnEndOfUtterance needed)
	// This is used when SingleUtterance=false - Google sends multiple finals per stream
	continuousMode bool

	// Observability
	metrics *metrics.Metrics
	logger  zerolog.Logger
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
	logger := log.With().
		Str("component", "audio.Handler").
		Str("interactionId", interactionId).
		Str("tenantId", tenantId).
		Str("segmentId", segmentId).
		Logger()

	// Record segment creation
	m := metrics.DefaultMetrics
	m.RecordSegmentCreated()

	return &Handler{
		adapter:          adapter,
		publisher:        publisher,
		segmentGen:       segmentGen,
		interactionId:    interactionId,
		tenantId:         tenantId,
		lifecycle:        segment.NewLifecycle(segmentId),
		limits:           limits,
		segmentStartTime: time.Now(),
		metrics:          m,
		logger:           logger,
	}
}

// SetSegmentTransitionCallback sets a callback for when utterance boundaries are detected.
// This allows the server to handle segment transitions (e.g., create new STT session).
func (h *Handler) SetSegmentTransitionCallback(cb SegmentTransitionCallback) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onSegmentTransition = cb
}

// SetContinuousMode enables continuous mode where each final creates a new segment.
// This is used when SingleUtterance=false - Google sends multiple finals per stream
// without END_OF_SINGLE_UTTERANCE events.
func (h *Handler) SetContinuousMode(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.continuousMode = enabled
}

// Start begins the STT session with this handler as the callback receiver.
func (h *Handler) Start(ctx context.Context) error {
	h.logger.Info().Msg("Starting audio handler")
	h.ctx = ctx // Store context for adapter restart on utterance boundaries
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

	// Record audio metrics
	h.metrics.RecordAudioReceived(len(audio))

	// Check audio bytes limit
	if h.limits.MaxAudioBytes > 0 && currentBytes > h.limits.MaxAudioBytes {
		reason := fmt.Sprintf("max audio bytes exceeded: %d > %d", currentBytes, h.limits.MaxAudioBytes)
		h.metrics.RecordLimitExceeded("audio_bytes")
		h.DropSegment(reason)
		return fmt.Errorf("segment limit exceeded: %s", reason)
	}

	// Check duration limit
	if h.limits.MaxDuration > 0 && time.Since(startTime) > h.limits.MaxDuration {
		reason := fmt.Sprintf("max duration exceeded: %v > %v", time.Since(startTime), h.limits.MaxDuration)
		h.metrics.RecordLimitExceeded("duration")
		h.DropSegment(reason)
		return fmt.Errorf("segment limit exceeded: %s", reason)
	}

	return h.adapter.SendAudio(ctx, audio)
}

// Close ends the STT session and closes the current segment.
func (h *Handler) Close() error {
	h.logger.Info().
		Str("segmentId", h.lifecycle.SegmentId()).
		Str("state", h.lifecycle.State().String()).
		Msg("Closing audio handler")

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
		h.logger.Warn().
			Str("segmentId", h.lifecycle.SegmentId()).
			Str("state", h.lifecycle.State().String()).
			Err(err).
			Msg("OnPartial ignored")
		return
	}

	// Track and check partial count limit
	h.mu.Lock()
	h.partialCount++
	count := h.partialCount
	h.mu.Unlock()

	if h.limits.MaxPartials > 0 && count > h.limits.MaxPartials {
		reason := fmt.Sprintf("max partials exceeded: %d > %d", count, h.limits.MaxPartials)
		h.metrics.RecordLimitExceeded("partials")
		h.DropSegment(reason)
		return
	}

	// Record metrics
	h.metrics.RecordPartialTranscript()

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
// If a restart is pending (from OnEndOfUtterance), this triggers the
// segment transition and adapter restart after processing the final.
func (h *Handler) OnFinal(text string, confidence float64) {
	// Validate state transition - this also transitions to FINAL_EMITTED
	if err := h.lifecycle.EmitFinal(); err != nil {
		h.logger.Warn().
			Str("segmentId", h.lifecycle.SegmentId()).
			Str("state", h.lifecycle.State().String()).
			Err(err).
			Msg("OnFinal ignored")
		return
	}

	h.mu.RLock()
	audioOffsetMs := h.lastAudioOffsetMs
	h.mu.RUnlock()

	// Record metrics
	h.metrics.RecordFinalTranscript()
	h.metrics.RecordSegmentCompleted()

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

	h.logger.Info().
		Str("segmentId", ev.SegmentID).
		Float64("confidence", confidence).
		Int("textLen", len(text)).
		Msg("Final transcript received")

	h.publishFinal(ev)

	// Check if we need to restart for the next utterance
	// This happens after OnEndOfUtterance was received (SingleUtterance mode)
	h.mu.Lock()
	needsRestart := h.pendingRestart
	h.pendingRestart = false
	isContinuousMode := h.continuousMode
	h.mu.Unlock()

	if needsRestart {
		// SingleUtterance mode: restart adapter for next utterance
		h.handleUtteranceTransition()
	} else if isContinuousMode {
		// Continuous mode: create new segment but don't restart adapter
		// (Google keeps streaming in the same session)
		h.handleContinuousModeTransition()
	}
}

// handleContinuousModeTransition creates a new segment after a final in continuous mode.
// Unlike handleUtteranceTransition, this doesn't restart the adapter since Google
// continues transcribing in the same session.
func (h *Handler) handleContinuousModeTransition() {
	oldSegmentId := h.lifecycle.SegmentId()

	// Close current segment
	h.lifecycle.Close()

	// Generate new segment ID and reset metrics
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

	// Record metrics
	h.metrics.RecordUtterance()
	h.metrics.RecordSegmentCreated()

	// Update logger context
	h.logger = h.logger.With().Str("segmentId", newSegmentId).Logger()

	h.logger.Debug().
		Str("oldSegmentId", oldSegmentId).
		Int("utteranceCount", h.utteranceCount).
		Int64("audioBytes", oldAudioBytes).
		Int("partialCount", oldPartialCount).
		Dur("duration", oldDuration).
		Msg("Continuous mode - new segment created after final")

	// Notify server of segment transition if callback is set
	if cb != nil {
		cb(newSegmentId)
	}
}

// handleUtteranceTransition manages the segment transition and adapter restart
// after an utterance boundary is detected and the final has been processed.
func (h *Handler) handleUtteranceTransition() {
	oldSegmentId := h.lifecycle.SegmentId()
	oldState := h.lifecycle.State()

	// Close current segment
	h.lifecycle.Close()

	// Generate new segment ID and reset metrics
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
	ctx := h.ctx
	h.mu.Unlock()

	// Reset lifecycle for new segment
	h.lifecycle.Reset(newSegmentId)

	// Record metrics
	h.metrics.RecordUtterance()
	h.metrics.RecordSegmentCreated()

	// Update logger context
	h.logger = h.logger.With().Str("segmentId", newSegmentId).Logger()

	h.logger.Info().
		Str("oldSegmentId", oldSegmentId).
		Str("oldState", oldState.String()).
		Int("utteranceCount", h.utteranceCount).
		Int64("audioBytes", oldAudioBytes).
		Int("partialCount", oldPartialCount).
		Dur("duration", oldDuration).
		Msg("Utterance complete - new segment created")

	// Restart the STT adapter for the next utterance
	// This is required for providers like Google with SingleUtterance mode
	if ctx != nil {
		if err := h.adapter.Restart(ctx); err != nil {
			h.logger.Error().
				Err(err).
				Str("segmentId", newSegmentId).
				Msg("Failed to restart STT adapter")
		} else {
			h.logger.Debug().
				Str("segmentId", newSegmentId).
				Msg("STT adapter restarted for next utterance")
		}
	}

	// Notify server of segment transition if callback is set
	if cb != nil {
		cb(newSegmentId)
	}
}

// OnEndOfUtterance is called when the STT provider detects end of speech.
// This signals the boundary between utterances within a conversation.
// Note: With Google's SingleUtterance mode, END_OF_SINGLE_UTTERANCE comes BEFORE
// the final result. We must wait for OnFinal before restarting, to avoid
// two Listen goroutines racing (old one sending final, new one sending partials).
func (h *Handler) OnEndOfUtterance() {
	h.mu.Lock()
	h.pendingRestart = true
	h.mu.Unlock()

	h.logger.Debug().
		Str("segmentId", h.lifecycle.SegmentId()).
		Msg("End of utterance detected - will restart after final")
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

	// Record metrics
	h.metrics.RecordSegmentDropped("stt_error")
	h.metrics.RecordSTTError("", "stream_error")

	h.logger.Error().
		Err(err).
		Str("segmentId", segmentId).
		Str("previousState", oldState.String()).
		Bool("dropped", dropped).
		Msg("STT error - segment DROPPED")
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

	if dropped {
		h.metrics.RecordSegmentDropped(reason)
	}

	h.logger.Warn().
		Str("segmentId", segmentId).
		Str("previousState", oldState.String()).
		Str("reason", reason).
		Bool("dropped", dropped).
		Msg("Segment DROPPED")

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
	// Use a timeout context for publishing - don't use stream context
	// because stream may close before publish completes
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := h.publisher.PublishPartial(ctx, h.interactionId, ev); err != nil {
		h.logger.Error().
			Err(err).
			Str("segmentId", ev.SegmentID).
			Msg("Failed to publish partial")
	}
}

func (h *Handler) publishFinal(ev models.TranscriptFinal) {
	// Use a timeout context for publishing - don't use stream context
	// because stream may close before publish completes
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := h.publisher.PublishFinal(ctx, h.interactionId, ev); err != nil {
		h.logger.Error().
			Err(err).
			Str("segmentId", ev.SegmentID).
			Msg("Failed to publish final")
	}
}
