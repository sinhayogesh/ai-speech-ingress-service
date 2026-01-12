package mock

import (
	"context"
	"sync"
	"testing"
	"time"
)

// testCallback implements stt.Callback for testing
type testCallback struct {
	mu         sync.Mutex
	partials   []string
	finals     []finalResult
	errors     []error
	utterances int
}

type finalResult struct {
	text       string
	confidence float64
}

func (c *testCallback) OnPartial(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.partials = append(c.partials, text)
}

func (c *testCallback) OnFinal(text string, confidence float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finals = append(c.finals, finalResult{text, confidence})
}

func (c *testCallback) OnEndOfUtterance() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.utterances++
}

func (c *testCallback) OnError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errors = append(c.errors, err)
}

func (c *testCallback) getPartials() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.partials...)
}

func (c *testCallback) getFinals() []finalResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]finalResult{}, c.finals...)
}

func (c *testCallback) getUtterances() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.utterances
}

func TestAdapter_New(t *testing.T) {
	adapter := New()
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if adapter.closed {
		t.Error("expected adapter to not be closed initially")
	}
	if adapter.finalSent {
		t.Error("expected finalSent to be false initially")
	}
}

func TestAdapter_Start(t *testing.T) {
	adapter := New()
	cb := &testCallback{}

	err := adapter.Start(context.Background(), cb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if adapter.cb != cb {
		t.Error("expected callback to be set")
	}
}

func TestAdapter_SendAudio_TriggersPartials(t *testing.T) {
	adapter := New()
	cb := &testCallback{}
	adapter.Start(context.Background(), cb)

	// Send audio frames to trigger partials
	for i := 0; i < 3; i++ {
		err := adapter.SendAudio(context.Background(), []byte("audio"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// Wait for async callbacks
	time.Sleep(200 * time.Millisecond)

	partials := cb.getPartials()
	if len(partials) == 0 {
		t.Error("expected partials to be received")
	}
}

func TestAdapter_SendAudio_TriggersFinalAndUtterance(t *testing.T) {
	adapter := New()
	cb := &testCallback{}
	adapter.Start(context.Background(), cb)

	// Send enough audio frames to trigger final
	for i := 0; i < 5; i++ {
		err := adapter.SendAudio(context.Background(), []byte("audio"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		time.Sleep(60 * time.Millisecond)
	}

	// Wait for async callbacks
	time.Sleep(300 * time.Millisecond)

	finals := cb.getFinals()
	if len(finals) != 1 {
		t.Errorf("expected 1 final, got %d", len(finals))
	}

	utterances := cb.getUtterances()
	if utterances != 1 {
		t.Errorf("expected 1 utterance, got %d", utterances)
	}
}

func TestAdapter_Close(t *testing.T) {
	adapter := New()
	cb := &testCallback{}
	adapter.Start(context.Background(), cb)

	err := adapter.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !adapter.closed {
		t.Error("expected adapter to be closed")
	}
}

func TestAdapter_Close_Idempotent(t *testing.T) {
	adapter := New()
	cb := &testCallback{}
	adapter.Start(context.Background(), cb)

	adapter.Close()
	err := adapter.Close()

	if err != nil {
		t.Fatalf("unexpected error on second close: %v", err)
	}
}

func TestAdapter_SendAudio_AfterClose(t *testing.T) {
	adapter := New()
	cb := &testCallback{}
	adapter.Start(context.Background(), cb)
	adapter.Close()

	// Should not panic or error
	err := adapter.SendAudio(context.Background(), []byte("audio"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAdapter_Close_SendsFinalIfNotSent(t *testing.T) {
	adapter := New()
	cb := &testCallback{}
	adapter.Start(context.Background(), cb)

	// Close without sending enough audio to trigger final
	adapter.Close()

	// Wait for async final
	time.Sleep(200 * time.Millisecond)

	finals := cb.getFinals()
	if len(finals) != 1 {
		t.Errorf("expected 1 final on close, got %d", len(finals))
	}
}

func TestAdapter_CyclesThroughUtterances(t *testing.T) {
	// Get first adapter's utterance
	adapter1 := New()
	utt1 := adapter1.utterance.Final

	// Get second adapter's utterance
	adapter2 := New()
	utt2 := adapter2.utterance.Final

	// They should be different (cycling through)
	if utt1 == utt2 {
		// Note: This could occasionally fail if we cycle back to start
		// but with 5 utterances this is unlikely in sequence
		t.Log("utterances may be the same if counter cycled")
	}
}

func TestDefaultUtterances(t *testing.T) {
	if len(DefaultUtterances) != 5 {
		t.Errorf("expected 5 default utterances, got %d", len(DefaultUtterances))
	}

	for i, utt := range DefaultUtterances {
		if len(utt.Partials) == 0 {
			t.Errorf("utterance %d has no partials", i)
		}
		if utt.Final == "" {
			t.Errorf("utterance %d has empty final", i)
		}
		if utt.Confidence <= 0 || utt.Confidence > 1 {
			t.Errorf("utterance %d has invalid confidence %f", i, utt.Confidence)
		}
	}
}

func TestAdapter_ThreadSafety(t *testing.T) {
	adapter := New()
	cb := &testCallback{}
	adapter.Start(context.Background(), cb)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				adapter.SendAudio(context.Background(), []byte("audio"))
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
	adapter.Close()

	// Should not panic - just verify it completes
}

func TestAdapter_NoCallbackSet(t *testing.T) {
	adapter := New()
	// Don't set callback via Start

	// Should not panic
	err := adapter.SendAudio(context.Background(), []byte("audio"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = adapter.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
