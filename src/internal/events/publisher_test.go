package events

import (
	"context"
	"testing"
)

func TestNew_DisabledMode(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
	}{
		{"disabled", &Config{Enabled: false, Brokers: []string{"localhost:9092"}}},
		{"no brokers", &Config{Enabled: true, Brokers: []string{}}},
		{"empty brokers", &Config{Enabled: true, Brokers: nil}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.cfg)
			if p == nil {
				t.Fatal("expected non-nil publisher")
			}
			if p.enabled {
				t.Error("expected publisher to be disabled")
			}
			if p.writerPartial != nil {
				t.Error("expected nil partial writer when disabled")
			}
			if p.writerFinal != nil {
				t.Error("expected nil final writer when disabled")
			}
		})
	}
}

func TestNew_ConfigValues(t *testing.T) {
	cfg := &Config{
		Enabled:      false,
		Brokers:      []string{"localhost:9092"},
		TopicPartial: "test.partial",
		TopicFinal:   "test.final",
		Principal:    "test-principal",
	}

	p := New(cfg)

	if p.principal != "test-principal" {
		t.Errorf("expected principal 'test-principal', got %s", p.principal)
	}
	if p.topicPartial != "test.partial" {
		t.Errorf("expected topic partial 'test.partial', got %s", p.topicPartial)
	}
	if p.topicFinal != "test.final" {
		t.Errorf("expected topic final 'test.final', got %s", p.topicFinal)
	}
}

func TestPublisher_PublishPartial_Disabled(t *testing.T) {
	p := New(&Config{Enabled: false})

	event := map[string]string{"text": "test partial"}
	err := p.PublishPartial(context.Background(), "test-key", event)

	if err != nil {
		t.Errorf("expected no error when disabled, got %v", err)
	}
}

func TestPublisher_PublishFinal_Disabled(t *testing.T) {
	p := New(&Config{Enabled: false})

	event := map[string]string{"text": "test final"}
	err := p.PublishFinal(context.Background(), "test-key", event)

	if err != nil {
		t.Errorf("expected no error when disabled, got %v", err)
	}
}

func TestPublisher_PublishPartial_InvalidJSON(t *testing.T) {
	p := New(&Config{Enabled: false})

	// Create an unmarshalable value (channel)
	event := make(chan int)
	err := p.PublishPartial(context.Background(), "test-key", event)

	if err == nil {
		t.Error("expected error for unmarshalable event")
	}
}

func TestPublisher_PublishFinal_InvalidJSON(t *testing.T) {
	p := New(&Config{Enabled: false})

	// Create an unmarshalable value (channel)
	event := make(chan int)
	err := p.PublishFinal(context.Background(), "test-key", event)

	if err == nil {
		t.Error("expected error for unmarshalable event")
	}
}

func TestPublisher_Close_NoWriters(t *testing.T) {
	p := New(&Config{Enabled: false})

	err := p.Close()
	if err != nil {
		t.Errorf("expected no error closing disabled publisher, got %v", err)
	}
}

func TestPublisher_Close_NilPublisher(t *testing.T) {
	p := &Publisher{
		writerPartial: nil,
		writerFinal:   nil,
	}

	err := p.Close()
	if err != nil {
		t.Errorf("expected no error closing publisher with nil writers, got %v", err)
	}
}

type testEvent struct {
	EventType     string `json:"eventType"`
	InteractionID string `json:"interactionId"`
	Text          string `json:"text"`
}

func TestPublisher_PublishPartial_ValidEvent(t *testing.T) {
	p := New(&Config{
		Enabled:      false,
		TopicPartial: "test.partial",
		Principal:    "test-svc",
	})

	event := testEvent{
		EventType:     "interaction.transcript.partial",
		InteractionID: "int-123",
		Text:          "hello world",
	}

	err := p.PublishPartial(context.Background(), "int-123", event)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestPublisher_PublishFinal_ValidEvent(t *testing.T) {
	p := New(&Config{
		Enabled:    false,
		TopicFinal: "test.final",
		Principal:  "test-svc",
	})

	event := testEvent{
		EventType:     "interaction.transcript.final",
		InteractionID: "int-123",
		Text:          "hello world",
	}

	err := p.PublishFinal(context.Background(), "int-123", event)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}
