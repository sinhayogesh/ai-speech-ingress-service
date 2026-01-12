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

// Publisher publishes transcript events to Kafka.
type Publisher struct {
	writer    *kafka.Writer
	principal string
	enabled   bool
}

// Config holds Kafka publisher configuration.
type Config struct {
	Brokers   []string
	Topic     string
	Principal string
	Enabled   bool
}

// New creates a new Kafka event publisher.
func New(cfg *Config) *Publisher {
	if cfg == nil || !cfg.Enabled || len(cfg.Brokers) == 0 {
		log.Println("[PUBLISHER] Kafka disabled, using log-only mode")
		return &Publisher{
			principal: cfg.Principal,
			enabled:   false,
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

	writer := &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Topic:        cfg.Topic,
		Balancer:     &kafka.LeastBytes{},
		BatchTimeout: 10 * time.Millisecond,
		WriteTimeout: 10 * time.Second,
		RequiredAcks: kafka.RequireOne,
		Transport: &kafka.Transport{
			Dial: dialer.DialFunc,
		},
	}

	log.Printf("[PUBLISHER] Kafka enabled: brokers=%v topic=%s", cfg.Brokers, cfg.Topic)

	return &Publisher{
		writer:    writer,
		principal: cfg.Principal,
		enabled:   true,
	}
}

// Publish publishes an event to Kafka.
func (p *Publisher) Publish(ctx context.Context, topic string, key string, event any) error {
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("[PUBLISHER] Failed to marshal event: %v", err)
		return err
	}

	// Log the event
	log.Printf("[PUBLISH] principal=%s topic=%s key=%s payload=%s", p.principal, topic, key, payload)

	// If Kafka is disabled, just log
	if !p.enabled || p.writer == nil {
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

	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		log.Printf("[PUBLISHER] Failed to write to Kafka: %v", err)
		return err
	}

	return nil
}

// Close closes the Kafka writer.
func (p *Publisher) Close() error {
	if p.writer != nil {
		return p.writer.Close()
	}
	return nil
}
