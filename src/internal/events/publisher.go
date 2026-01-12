package events

import (
	"context"
	"encoding/json"
	"log"
)

type Publisher struct {
	Principal string
}

func New(principal string) *Publisher {
	return &Publisher{Principal: principal}
}

func (p *Publisher) Publish(ctx context.Context, topic string, key string, event any) error {
	payload, _ := json.Marshal(event)
	log.Printf(
		"[PUBLISH] principal=%s topic=%s key=%s payload=%s",
		p.Principal, topic, key, payload,
	)
	return nil
}
