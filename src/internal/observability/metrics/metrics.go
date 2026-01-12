// Package metrics provides Prometheus metrics for observability.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "ai_speech_ingress"

// Metrics holds all Prometheus metrics for the service.
type Metrics struct {
	// Stream metrics
	StreamsTotal   prometheus.Counter
	StreamsActive  prometheus.Gauge
	StreamsSuccess prometheus.Counter
	StreamsFailed  prometheus.Counter
	StreamDuration prometheus.Histogram

	// Segment metrics
	SegmentsCreated   prometheus.Counter
	SegmentsCompleted prometheus.Counter
	SegmentsDropped   *prometheus.CounterVec

	// Transcript metrics
	TranscriptsPartial prometheus.Counter
	TranscriptsFinal   prometheus.Counter

	// Audio metrics
	AudioBytesReceived  prometheus.Counter
	AudioFramesReceived prometheus.Counter

	// Kafka publish metrics
	KafkaPublishTotal   *prometheus.CounterVec
	KafkaPublishErrors  *prometheus.CounterVec
	KafkaPublishLatency *prometheus.HistogramVec

	// STT metrics
	STTLatency         *prometheus.HistogramVec
	STTErrors          *prometheus.CounterVec
	STTUtteranceCount  prometheus.Counter
	STTPartialLatency  prometheus.Histogram
	STTFinalLatency    prometheus.Histogram

	// Backpressure metrics
	SegmentLimitExceeded *prometheus.CounterVec
}

// DefaultMetrics is the global metrics instance.
var DefaultMetrics = NewMetrics()

// NewMetrics creates and registers all Prometheus metrics.
func NewMetrics() *Metrics {
	return &Metrics{
		// Stream metrics
		StreamsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "streams_total",
			Help:      "Total number of gRPC streams started",
		}),
		StreamsActive: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "streams_active",
			Help:      "Number of currently active gRPC streams",
		}),
		StreamsSuccess: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "streams_success_total",
			Help:      "Total number of successfully completed streams",
		}),
		StreamsFailed: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "streams_failed_total",
			Help:      "Total number of failed streams",
		}),
		StreamDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "stream_duration_seconds",
			Help:      "Duration of gRPC streams in seconds",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		}),

		// Segment metrics
		SegmentsCreated: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "segments_created_total",
			Help:      "Total number of segments created",
		}),
		SegmentsCompleted: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "segments_completed_total",
			Help:      "Total number of segments completed with final transcript",
		}),
		SegmentsDropped: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "segments_dropped_total",
			Help:      "Total number of segments dropped",
		}, []string{"reason"}),

		// Transcript metrics
		TranscriptsPartial: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "transcripts_partial_total",
			Help:      "Total number of partial transcripts received",
		}),
		TranscriptsFinal: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "transcripts_final_total",
			Help:      "Total number of final transcripts received",
		}),

		// Audio metrics
		AudioBytesReceived: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "audio_bytes_received_total",
			Help:      "Total audio bytes received",
		}),
		AudioFramesReceived: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "audio_frames_received_total",
			Help:      "Total audio frames received",
		}),

		// Kafka publish metrics
		KafkaPublishTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "kafka_publish_total",
			Help:      "Total number of Kafka messages published",
		}, []string{"topic", "event_type"}),
		KafkaPublishErrors: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "kafka_publish_errors_total",
			Help:      "Total number of Kafka publish errors",
		}, []string{"topic", "event_type"}),
		KafkaPublishLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "kafka_publish_latency_seconds",
			Help:      "Kafka publish latency in seconds",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		}, []string{"topic"}),

		// STT metrics
		STTLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "stt_latency_seconds",
			Help:      "Speech-to-text processing latency in seconds",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5},
		}, []string{"provider", "type"}),
		STTErrors: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "stt_errors_total",
			Help:      "Total number of STT errors",
		}, []string{"provider", "error_type"}),
		STTUtteranceCount: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "stt_utterances_total",
			Help:      "Total number of utterances detected",
		}),
		STTPartialLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "stt_partial_latency_seconds",
			Help:      "Time from audio send to partial transcript",
			Buckets:   []float64{0.05, 0.1, 0.2, 0.3, 0.5, 1},
		}),
		STTFinalLatency: promauto.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "stt_final_latency_seconds",
			Help:      "Time from audio send to final transcript",
			Buckets:   []float64{0.1, 0.25, 0.5, 1, 2, 5},
		}),

		// Backpressure metrics
		SegmentLimitExceeded: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "segment_limit_exceeded_total",
			Help:      "Total number of times segment limits were exceeded",
		}, []string{"limit_type"}),
	}
}

// RecordStreamStart records a new stream starting.
func (m *Metrics) RecordStreamStart() {
	m.StreamsTotal.Inc()
	m.StreamsActive.Inc()
}

// RecordStreamEnd records a stream ending.
func (m *Metrics) RecordStreamEnd(success bool, durationSeconds float64) {
	m.StreamsActive.Dec()
	m.StreamDuration.Observe(durationSeconds)
	if success {
		m.StreamsSuccess.Inc()
	} else {
		m.StreamsFailed.Inc()
	}
}

// RecordSegmentCreated records a new segment being created.
func (m *Metrics) RecordSegmentCreated() {
	m.SegmentsCreated.Inc()
}

// RecordSegmentCompleted records a segment completed with final transcript.
func (m *Metrics) RecordSegmentCompleted() {
	m.SegmentsCompleted.Inc()
}

// RecordSegmentDropped records a segment being dropped.
func (m *Metrics) RecordSegmentDropped(reason string) {
	m.SegmentsDropped.WithLabelValues(reason).Inc()
}

// RecordPartialTranscript records a partial transcript received.
func (m *Metrics) RecordPartialTranscript() {
	m.TranscriptsPartial.Inc()
}

// RecordFinalTranscript records a final transcript received.
func (m *Metrics) RecordFinalTranscript() {
	m.TranscriptsFinal.Inc()
}

// RecordAudioReceived records audio bytes and frames received.
func (m *Metrics) RecordAudioReceived(bytes int) {
	m.AudioBytesReceived.Add(float64(bytes))
	m.AudioFramesReceived.Inc()
}

// RecordKafkaPublish records a Kafka publish attempt.
func (m *Metrics) RecordKafkaPublish(topic, eventType string, err error, latencySeconds float64) {
	m.KafkaPublishTotal.WithLabelValues(topic, eventType).Inc()
	m.KafkaPublishLatency.WithLabelValues(topic).Observe(latencySeconds)
	if err != nil {
		m.KafkaPublishErrors.WithLabelValues(topic, eventType).Inc()
	}
}

// RecordSTTError records an STT error.
func (m *Metrics) RecordSTTError(provider, errorType string) {
	m.STTErrors.WithLabelValues(provider, errorType).Inc()
}

// RecordUtterance records an utterance boundary detection.
func (m *Metrics) RecordUtterance() {
	m.STTUtteranceCount.Inc()
}

// RecordLimitExceeded records when a segment limit is exceeded.
func (m *Metrics) RecordLimitExceeded(limitType string) {
	m.SegmentLimitExceeded.WithLabelValues(limitType).Inc()
}

