// Package events provides event publishing functionality.
package events

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/segmentio/kafka-go"

	"ai-speech-ingress-service/internal/observability/metrics"
)

// Publisher publishes transcript events to separate Kafka topics.
type Publisher struct {
	writerPartial *kafka.Writer
	writerFinal   *kafka.Writer
	principal     string
	topicPartial  string
	topicFinal    string
	enabled       bool
	metrics       *metrics.Metrics
}

// Config holds Kafka publisher configuration.
type Config struct {
	Brokers      []string
	TopicPartial string
	TopicFinal   string
	Principal    string
	Enabled      bool
}

// New creates a new Kafka event publisher with separate topics for partial and final transcripts.
func New(cfg *Config) *Publisher {
	m := metrics.DefaultMetrics

	// Handle nil config case
	if cfg == nil {
		log.Info().Msg("Kafka disabled (nil config), using log-only mode")
		return &Publisher{
			enabled: false,
			metrics: m,
		}
	}

	if !cfg.Enabled || len(cfg.Brokers) == 0 {
		log.Info().Msg("Kafka disabled, using log-only mode")
		return &Publisher{
			principal:    cfg.Principal,
			topicPartial: cfg.TopicPartial,
			topicFinal:   cfg.TopicFinal,
			enabled:      false,
			metrics:      m,
		}
	}

	// Create a custom dialer with longer timeouts
	// Create a custom dialer with longer timeouts for DNS resolution in Kubernetes
	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
	}

	transport := &kafka.Transport{
		Dial: dialer.DialFunc,
	}

	// Writer for partial transcripts
	writerPartial := &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Topic:        cfg.TopicPartial,
		Balancer:     &kafka.LeastBytes{},
		BatchTimeout: 10 * time.Millisecond,
		WriteTimeout: 10 * time.Second,
		RequiredAcks: kafka.RequireOne,
		Transport:    transport,
	}

	// Writer for final transcripts
	writerFinal := &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Topic:        cfg.TopicFinal,
		Balancer:     &kafka.LeastBytes{},
		BatchTimeout: 10 * time.Millisecond,
		WriteTimeout: 10 * time.Second,
		RequiredAcks: kafka.RequireOne,
		Transport:    transport,
	}

	log.Info().
		Strs("brokers", cfg.Brokers).
		Str("topicPartial", cfg.TopicPartial).
		Str("topicFinal", cfg.TopicFinal).
		Str("principal", cfg.Principal).
		Msg("Kafka publisher initialized")

	return &Publisher{
		writerPartial: writerPartial,
		writerFinal:   writerFinal,
		principal:     cfg.Principal,
		topicPartial:  cfg.TopicPartial,
		topicFinal:    cfg.TopicFinal,
		enabled:       true,
		metrics:       m,
	}
}

// PublishPartial publishes a partial transcript event to the partial topic.
func (p *Publisher) PublishPartial(ctx context.Context, key string, event any) error {
	return p.publish(ctx, p.writerPartial, p.topicPartial, "partial", key, event)
}

// PublishFinal publishes a final transcript event to the final topic.
func (p *Publisher) PublishFinal(ctx context.Context, key string, event any) error {
	return p.publish(ctx, p.writerFinal, p.topicFinal, "final", key, event)
}

// publish is the internal method that writes to a specific Kafka writer.
func (p *Publisher) publish(ctx context.Context, writer *kafka.Writer, topic, eventType, key string, event any) error {
	start := time.Now()

	payload, err := json.Marshal(event)
	if err != nil {
		log.Error().Err(err).Str("topic", topic).Msg("Failed to marshal event")
		return err
	}

	// Log the event
	log.Debug().
		Str("principal", p.principal).
		Str("topic", topic).
		Str("key", key).
		RawJSON("payload", payload).
		Msg("Publishing event")

	// If Kafka is disabled, just log
	if !p.enabled || writer == nil {
		p.metrics.RecordKafkaPublish(topic, eventType, nil, time.Since(start).Seconds())
		return nil
	}

	// Publish to Kafka
	msg := kafka.Message{
		Key:   []byte(key),
		Value: payload,
		Headers: []kafka.Header{
			{Key: "eventType", Value: []byte(topic)},
			{Key: "principal", Value: []byte(p.principal)},
		},
	}

	if err := writer.WriteMessages(ctx, msg); err != nil {
		log.Error().
			Err(err).
			Str("topic", topic).
			Str("key", key).
			Msg("Failed to write to Kafka")
		p.metrics.RecordKafkaPublish(topic, eventType, err, time.Since(start).Seconds())
		return err
	}

	p.metrics.RecordKafkaPublish(topic, eventType, nil, time.Since(start).Seconds())
	return nil
}

// Close closes both Kafka writers.
func (p *Publisher) Close() error {
	var err error
	if p.writerPartial != nil {
		if e := p.writerPartial.Close(); e != nil {
			log.Error().Err(e).Msg("Error closing partial writer")
			err = e
		}
	}
	if p.writerFinal != nil {
		if e := p.writerFinal.Close(); e != nil {
			log.Error().Err(e).Msg("Error closing final writer")
			err = e
		}
	}
	return err
}
