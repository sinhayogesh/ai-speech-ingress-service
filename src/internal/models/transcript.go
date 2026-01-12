// Package models defines the data structures for transcript events.
package models

// TranscriptPartial represents an interim/partial transcript result.
type TranscriptPartial struct {
	EventType     string `json:"eventType"`
	InteractionID string `json:"interactionId"`
	TenantID      string `json:"tenantId"`
	Timestamp     int64  `json:"timestamp"`
	SegmentID     string `json:"segmentId"`
	Text          string `json:"text"`
}

// TranscriptFinal represents a final transcript result with confidence score.
type TranscriptFinal struct {
	EventType     string  `json:"eventType"`
	InteractionID string  `json:"interactionId"`
	TenantID      string  `json:"tenantId"`
	Timestamp     int64   `json:"timestamp"`
	SegmentID     string  `json:"segmentId"`
	Text          string  `json:"text"`
	Confidence    float64 `json:"confidence"`
	AudioOffsetMs int64   `json:"audioOffsetMs"`
}
