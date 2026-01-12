package audio

import (
	"context"
	"testing"
	"time"

	"ai-speech-ingress-service/internal/events"
	"ai-speech-ingress-service/internal/service/segment"
	"ai-speech-ingress-service/internal/service/stt"
)

// testAdapter implements stt.Adapter for testing
type testAdapter struct {
	started bool
	closed  bool
	audio   [][]byte
	cb      stt.Callback
}

func (m *testAdapter) Start(ctx context.Context, cb stt.Callback) error {
	m.started = true
	m.cb = cb
	return nil
}

func (m *testAdapter) SendAudio(ctx context.Context, audio []byte) error {
	m.audio = append(m.audio, audio)
	return nil
}

func (m *testAdapter) Close() error {
	m.closed = true
	return nil
}

// mockPublisher for testing (no-op)
func newMockPublisher() *events.Publisher {
	return events.New(&events.Config{Enabled: false})
}

func TestHandler_MaxAudioBytesLimit(t *testing.T) {
	adapter := &testAdapter{}
	publisher := newMockPublisher()
	segGen := segment.New()

	limits := SegmentLimits{
		MaxAudioBytes: 100, // 100 bytes max
		MaxDuration:   time.Hour,
		MaxPartials:   1000,
	}

	handler := NewHandlerWithLimits(adapter, publisher, segGen, "int-1", "tenant-1", "seg-1", limits)

	ctx := context.Background()

	// Send 50 bytes - should succeed
	err := handler.SendAudio(ctx, make([]byte, 50), 0)
	if err != nil {
		t.Fatalf("First send should succeed: %v", err)
	}

	// Send 60 more bytes (total 110) - should fail
	err = handler.SendAudio(ctx, make([]byte, 60), 100)
	if err == nil {
		t.Fatal("Expected error when exceeding max audio bytes")
	}

	// Segment should be dropped
	if !handler.IsSegmentDropped() {
		t.Error("Segment should be dropped after exceeding limit")
	}
}

func TestHandler_MaxPartialsLimit(t *testing.T) {
	adapter := &testAdapter{}
	publisher := newMockPublisher()
	segGen := segment.New()

	limits := SegmentLimits{
		MaxAudioBytes: 1024 * 1024,
		MaxDuration:   time.Hour,
		MaxPartials:   3, // 3 partials max
	}

	handler := NewHandlerWithLimits(adapter, publisher, segGen, "int-1", "tenant-1", "seg-1", limits)

	// Send 3 partials - should all succeed
	for i := 0; i < 3; i++ {
		handler.OnPartial("partial text")
	}

	if handler.IsSegmentDropped() {
		t.Error("Segment should not be dropped after 3 partials")
	}

	// 4th partial should cause drop
	handler.OnPartial("one too many")

	if !handler.IsSegmentDropped() {
		t.Error("Segment should be dropped after exceeding max partials")
	}
}

func TestHandler_MaxDurationLimit(t *testing.T) {
	adapter := &testAdapter{}
	publisher := newMockPublisher()
	segGen := segment.New()

	limits := SegmentLimits{
		MaxAudioBytes: 1024 * 1024,
		MaxDuration:   50 * time.Millisecond, // 50ms max
		MaxPartials:   1000,
	}

	handler := NewHandlerWithLimits(adapter, publisher, segGen, "int-1", "tenant-1", "seg-1", limits)

	ctx := context.Background()

	// First send - should succeed (within duration)
	err := handler.SendAudio(ctx, []byte("audio"), 0)
	if err != nil {
		t.Fatalf("First send should succeed: %v", err)
	}

	// Wait for duration to exceed
	time.Sleep(60 * time.Millisecond)

	// Next send should fail due to duration limit
	err = handler.SendAudio(ctx, []byte("audio"), 100)
	if err == nil {
		t.Fatal("Expected error when exceeding max duration")
	}

	if !handler.IsSegmentDropped() {
		t.Error("Segment should be dropped after exceeding duration limit")
	}
}

func TestHandler_MetricsReset(t *testing.T) {
	adapter := &testAdapter{}
	publisher := newMockPublisher()
	segGen := segment.New()

	limits := DefaultLimits()

	handler := NewHandlerWithLimits(adapter, publisher, segGen, "int-1", "tenant-1", "seg-1", limits)

	ctx := context.Background()

	// Send some audio and partials
	handler.SendAudio(ctx, make([]byte, 100), 0)
	handler.OnPartial("partial 1")
	handler.OnPartial("partial 2")

	metrics := handler.GetSegmentMetrics()
	if metrics.AudioBytes != 100 {
		t.Errorf("Expected 100 audio bytes, got %d", metrics.AudioBytes)
	}
	if metrics.PartialCount != 2 {
		t.Errorf("Expected 2 partials, got %d", metrics.PartialCount)
	}

	// Simulate end of utterance - metrics should reset
	handler.OnEndOfUtterance()

	metrics = handler.GetSegmentMetrics()
	if metrics.AudioBytes != 0 {
		t.Errorf("Expected 0 audio bytes after reset, got %d", metrics.AudioBytes)
	}
	if metrics.PartialCount != 0 {
		t.Errorf("Expected 0 partials after reset, got %d", metrics.PartialCount)
	}
}

func TestHandler_DefaultLimits(t *testing.T) {
	limits := DefaultLimits()

	if limits.MaxAudioBytes != 5*1024*1024 {
		t.Errorf("Expected default max audio bytes to be 5MB, got %d", limits.MaxAudioBytes)
	}
	if limits.MaxDuration != 5*time.Minute {
		t.Errorf("Expected default max duration to be 5min, got %v", limits.MaxDuration)
	}
	if limits.MaxPartials != 500 {
		t.Errorf("Expected default max partials to be 500, got %d", limits.MaxPartials)
	}
}
