// Package events provides event publishing functionality.
package events

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"time"

	"github.com/segmentio/kafka-go"
)

// Publisher publishes transcript events to separate Kafka topics.
type Publisher struct {
	writerPartial *kafka.Writer
	writerFinal   *kafka.Writer
	principal     string
	topicPartial  string
	topicFinal    string
	enabled       bool
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
	if cfg == nil || !cfg.Enabled || len(cfg.Brokers) == 0 {
		log.Println("[PUBLISHER] Kafka disabled, using log-only mode")
		return &Publisher{
			principal:    cfg.Principal,
			topicPartial: cfg.TopicPartial,
			topicFinal:   cfg.TopicFinal,
			enabled:      false,
		}
	}

	// Create a custom dialer with longer timeouts for DNS resolution in Kubernetes
	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
		Resolver: &net.Resolver{
			PreferGo: true,
		},
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

	log.Printf("[PUBLISHER] Kafka enabled: brokers=%v topicPartial=%s topicFinal=%s",
		cfg.Brokers, cfg.TopicPartial, cfg.TopicFinal)

	return &Publisher{
		writerPartial: writerPartial,
		writerFinal:   writerFinal,
		principal:     cfg.Principal,
		topicPartial:  cfg.TopicPartial,
		topicFinal:    cfg.TopicFinal,
		enabled:       true,
	}
}

// PublishPartial publishes a partial transcript event to the partial topic.
func (p *Publisher) PublishPartial(ctx context.Context, key string, event any) error {
	return p.publish(ctx, p.writerPartial, p.topicPartial, key, event)
}

// PublishFinal publishes a final transcript event to the final topic.
func (p *Publisher) PublishFinal(ctx context.Context, key string, event any) error {
	return p.publish(ctx, p.writerFinal, p.topicFinal, key, event)
}

// publish is the internal method that writes to a specific Kafka writer.
func (p *Publisher) publish(ctx context.Context, writer *kafka.Writer, topic string, key string, event any) error {
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("[PUBLISHER] Failed to marshal event: %v", err)
		return err
	}

	// Log the event
	log.Printf("[PUBLISH] principal=%s topic=%s key=%s payload=%s", p.principal, topic, key, payload)

	// If Kafka is disabled, just log
	if !p.enabled || writer == nil {
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
		log.Printf("[PUBLISHER] Failed to write to Kafka topic=%s: %v", topic, err)
		return err
	}

	return nil
}

// Close closes both Kafka writers.
func (p *Publisher) Close() error {
	var err error
	if p.writerPartial != nil {
		if e := p.writerPartial.Close(); e != nil {
			err = e
		}
	}
	if p.writerFinal != nil {
		if e := p.writerFinal.Close(); e != nil {
			err = e
		}
	}
	return err
}

