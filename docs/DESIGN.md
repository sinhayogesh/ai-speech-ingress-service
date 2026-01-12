# AI Speech Ingress Service - Design Document

## Overview

The AI Speech Ingress Service is responsible for receiving real-time audio streams, transcribing them using Speech-to-Text (STT) providers, and publishing transcript events to Kafka for downstream consumers.

## Goals

1. **Low Latency** - Stream audio in real-time with minimal delay
2. **Multi-Provider** - Support multiple STT providers (Google, Azure, AWS)
3. **Reliability** - Exactly-once final transcripts per utterance
4. **Scalability** - Horizontal scaling with stateless design
5. **Observability** - Structured logging, metrics, tracing

## Non-Goals

1. Audio recording/storage (handled by separate service)
2. Natural Language Understanding (downstream consumer responsibility)
3. Speaker diarization (future enhancement)

---

## Architecture

### High-Level Flow

```
┌─────────────┐    ┌─────────────────┐    ┌─────────────┐    ┌─────────┐
│  Telephony  │───▶│  Speech Ingress │───▶│  STT Cloud  │───▶│  Kafka  │
│   Gateway   │    │     Service     │    │  Provider   │    │         │
└─────────────┘    └─────────────────┘    └─────────────┘    └─────────┘
      │                    │                     │                │
   Audio              gRPC Stream          Websocket/gRPC     Events
   (RTP)              (AudioFrame)         (Audio + Text)   (JSON)
```

### Components

#### 1. gRPC Server (`internal/api/grpc/`)

- Implements `AudioStreamService.StreamAudio` RPC
- Client-streaming: receives `AudioFrame` messages
- Extracts `interactionId`, `tenantId` from first frame
- Creates per-stream handler and STT adapter

#### 2. Audio Handler (`internal/service/audio/`)

- Coordinates between STT adapter and event publisher
- Implements `stt.Callback` interface
- Manages segment lifecycle:
  - Creates initial segment on stream start
  - Transitions to new segment on `OnEndOfUtterance()`
  - Finalizes segment on stream close

#### 3. STT Adapter (`internal/service/stt/`)

Abstract interface for STT providers:

```go
type Adapter interface {
    Start(ctx context.Context, cb Callback) error
    SendAudio(ctx context.Context, audio []byte) error
    Close() error
}

type Callback interface {
    OnPartial(text string)
    OnFinal(text string, confidence float64)
    OnEndOfUtterance()
    OnError(err error)
}
```

**Implementations:**

| Provider | File | Notes |
|----------|------|-------|
| Mock | `stt/mock/adapter.go` | Cycles through 5 sample utterances |
| Google | `stt/google/adapter.go` | Uses `SingleUtterance` mode |

#### 4. Segment Generator & Lifecycle (`internal/service/segment/`)

**Generator** - Thread-safe generator for unique segment IDs:
- Format: `{interactionId}-seg-{n}`
- Uses atomic counter for uniqueness
- Shared across all streams for a given interaction

**Lifecycle State Machine** - Explicit state management for segments:

```
┌────────────────────────────────────────────────────────────────┐
│                    Segment Lifecycle                           │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│   ┌──────────┐    EmitFinal()    ┌────────────────┐           │
│   │   OPEN   │──────────────────▶│ FINAL_EMITTED  │           │
│   └──────────┘                   └────────────────┘           │
│        │                                │                      │
│        │ EmitPartial()                  │ Close()              │
│        │ (multiple)                     ▼                      │
│        ▼                         ┌──────────────┐             │
│   [emit partial]                 │    CLOSED    │             │
│        │                         │   (normal)   │             │
│        │                         └──────────────┘             │
│        │ Drop()                                               │
│        │ (error)                                              │
│        ▼                                                      │
│   ┌──────────────────────────────────────────┐                │
│   │               DROPPED                     │                │
│   │    (error - no final emitted)             │                │
│   │    "Silence > bad data"                   │                │
│   └──────────────────────────────────────────┘                │
│                                                                │
└────────────────────────────────────────────────────────────────┘
```

**States:**

| State | Can Emit Partial | Can Emit Final | Description |
|-------|-----------------|----------------|-------------|
| `OPEN` | ✅ Yes (multiple) | ✅ Yes (once) | Active segment |
| `FINAL_EMITTED` | ❌ No | ❌ No | Final sent, waiting to close |
| `CLOSED` | ❌ No | ❌ No | Segment complete (normal) |
| `DROPPED` | ❌ No | ❌ No | Segment abandoned (error) |

**Terminal states:** `CLOSED` and `DROPPED` are both terminal - no further operations allowed.

**Rules enforced:**
- Partials only in OPEN state
- Final only once (OPEN → FINAL_EMITTED)
- No events after CLOSED or DROPPED
- Drop() can be called from any non-terminal state
- Thread-safe via mutex

**Code:**

```go
// Validate before emitting
if err := h.lifecycle.EmitPartial(); err != nil {
    log.Printf("OnPartial ignored: state=%s err=%v", h.lifecycle.State(), err)
    return
}
// ... emit event
```

#### 5. Event Publisher (`internal/events/`)

Publishes to separate Kafka topics:
- `interaction.transcript.partial` - Interim results
- `interaction.transcript.final` - Confirmed results

Features:
- Custom dialer with 10s timeout for DNS resolution in K8s
- Configurable via environment variables
- Falls back to log-only mode if Kafka disabled

---

## Data Model

### Hierarchy

```
interactionId (call/conversation)
├── segmentId = {interactionId}-seg-1 (utterance #1)
│   ├── partial: "I want"
│   ├── partial: "I want to cancel"
│   └── final: "I want to cancel my subscription" ← exactly once
├── segmentId = {interactionId}-seg-2 (utterance #2)
│   ├── partial: "Yes"
│   └── final: "Yes please go ahead"
└── ...
```

### Event Schemas

#### TranscriptPartial

```json
{
  "eventType": "interaction.transcript.partial",
  "interactionId": "call-abc-123",
  "tenantId": "tenant-456",
  "segmentId": "call-abc-123-seg-1",
  "text": "I want to cancel",
  "timestamp": 1736697600000
}
```

#### TranscriptFinal

```json
{
  "eventType": "interaction.transcript.final",
  "interactionId": "call-abc-123",
  "tenantId": "tenant-456",
  "segmentId": "call-abc-123-seg-1",
  "text": "I want to cancel my subscription",
  "confidence": 0.94,
  "audioOffsetMs": 18420,
  "timestamp": 1736697600000
}
```

---

## Utterance Boundary Detection

### Problem

A single audio stream may contain multiple utterances (speaker pauses between sentences). We need to:
1. Detect when the speaker stops talking
2. Finalize the current segment
3. Start a new segment for subsequent speech

### Solution

#### Google STT

Uses `SingleUtterance: true` in streaming config:
- Google detects end-of-speech via Voice Activity Detection (VAD)
- Returns `END_OF_SINGLE_UTTERANCE` event
- We call `OnEndOfUtterance()` callback

#### Mock STT

Simulates utterance completion:
- After all partials sent (based on frame count)
- Sends final + `OnEndOfUtterance()`

### Handler Behavior

When `OnEndOfUtterance()` is called:
1. Log the segment transition
2. Generate new segmentId via segment generator
3. Update handler's current segmentId
4. Continue processing audio in new segment

---

## Kafka Topic Design

### Separate Topics

| Topic | Purpose | Consumers |
|-------|---------|-----------|
| `interaction.transcript.partial` | Real-time interim results | Live dashboards, agents |
| `interaction.transcript.final` | Confirmed transcripts | Analytics, storage, AI |

### Benefits

1. **ACL Separation** - Different consumers get different access
2. **Different Retention** - Partials can be short-lived
3. **Consumer Groups** - Independent scaling
4. **Filtering** - No need to filter by eventType

### Message Key

All messages keyed by `interactionId` for:
- Partition affinity (all segments of a call go to same partition)
- Ordered processing per interaction
- Log compaction compatibility

---

## Error Handling

### Design Principle: "Silence > Bad Data"

When errors occur, we prefer to emit **nothing** rather than potentially incorrect or incomplete data. This ensures downstream consumers never receive corrupted transcripts.

### Stream Error Scenarios

| Scenario | Behavior | Result |
|----------|----------|--------|
| **STT error mid-utterance** | Drop segment | No final emitted |
| **gRPC client disconnect** | Drop segment | No final emitted |
| **Network hiccups** | Drop segment | No final emitted |
| **Partial stream without final** | Drop segment | Partials orphaned |
| **Context cancelled** | Drop segment | No final emitted |

### Segment Drop Behavior

When `Drop()` is called:
1. Segment transitions to `DROPPED` state
2. No final transcript is emitted
3. Any pending partials are orphaned (consumers should handle)
4. Detailed log entry with:
   - `interactionId`
   - `segmentId`
   - Previous state
   - Drop reason

```go
func (h *Handler) OnError(err error) {
    // Drop the segment - no final will be emitted
    dropped := h.lifecycle.Drop()
    log.Printf("STT error - segment DROPPED: interactionId=%s segmentId=%s dropped=%v err=%v",
        h.interactionId, h.lifecycle.SegmentId(), dropped, err)
}
```

### gRPC Error Classification

Stream errors are classified for better observability:

| gRPC Code | Classification |
|-----------|---------------|
| `Canceled` | Client disconnect |
| `DeadlineExceeded` | Timeout |
| `Unavailable` | Network error |
| `ResourceExhausted` | Rate limiting |
| `Internal` | Server error |
| EOF | Unexpected close |

```go
func classifyStreamError(err error) string {
    if st, ok := status.FromError(err); ok {
        switch st.Code() {
        case codes.Canceled:
            return "client disconnect (gRPC canceled)"
        case codes.Unavailable:
            return "network error (unavailable)"
        // ... more cases
        }
    }
    return "stream error: " + err.Error()
}
```

### Recovery Strategy

After a segment is dropped, the stream can continue:
- For single-utterance mode: Stream typically ends
- For continuous mode: New segment starts fresh

**No retry on drop** - The audio data is ephemeral; retrying would require client re-send.

### Kafka Errors

```go
if err := p.writer.WriteMessages(ctx, msg); err != nil {
    log.Printf("[PUBLISHER] Failed to write to Kafka topic=%s: %v", topic, err)
    return err
}
```

Current: Log and return error. Future: Dead-letter queue, retries.

---

## Backpressure & Buffering Limits

Safety guardrails to prevent unbounded resource usage. These are not optimizations—they are critical for production stability.

### Why Limits Are Necessary

Without limits, a misbehaving client or network issue could:
- **Exhaust memory** (unbounded audio buffering)
- **Overwhelm downstream systems** (too many partials)
- **Create zombie segments** (never-ending streams)

### Configured Limits

| Limit | Default | Env Variable | Purpose |
|-------|---------|--------------|---------|
| **Max Audio Bytes** | 5MB | `SEGMENT_MAX_AUDIO_BYTES` | Prevent memory exhaustion |
| **Max Duration** | 5 minutes | `SEGMENT_MAX_DURATION` | Prevent zombie segments |
| **Max Partials** | 500 | `SEGMENT_MAX_PARTIALS` | Prevent downstream flood |

### Enforcement Behavior

When any limit is exceeded:
1. **Segment is DROPPED** immediately
2. **No final transcript** is emitted
3. **Detailed log** with limit that was exceeded
4. **Error returned** to caller (for audio/duration)

```go
// Audio bytes limit check
if h.limits.MaxAudioBytes > 0 && currentBytes > h.limits.MaxAudioBytes {
    reason := fmt.Sprintf("max audio bytes exceeded: %d > %d", currentBytes, h.limits.MaxAudioBytes)
    h.DropSegment(reason)
    return fmt.Errorf("segment limit exceeded: %s", reason)
}
```

### Metrics Tracking

Each segment tracks:
- `audioBytes` - Total audio bytes received
- `partialCount` - Number of partial transcripts
- `duration` - Time since segment started

Metrics are logged on segment completion and reset on new segment.

### Default Values Rationale

| Limit | Value | Reasoning |
|-------|-------|-----------|
| 5MB audio | ~625 seconds at 8kHz 16-bit mono | Well beyond reasonable utterance length |
| 5 minutes | Safety cap | No single utterance should exceed this |
| 500 partials | ~1 per 600ms | Typical STT sends partials every 200-500ms |

### Configuration

Override via environment variables:

```bash
export SEGMENT_MAX_AUDIO_BYTES=10485760  # 10MB
export SEGMENT_MAX_DURATION=10m          # 10 minutes
export SEGMENT_MAX_PARTIALS=1000         # 1000 partials
```

---

## Testing

### Mock STT Adapter

Provides realistic simulation without cloud credentials:
- 5 sample utterances with varying lengths
- Progressive partials (word-by-word)
- Configurable confidence scores
- Utterance boundary simulation

### Test Client

```bash
make test-client
```

Sends 6 audio frames to trigger:
- 3 frames: Partials sent
- Frame 4+: Final + EndOfUtterance
- Verifies segment transitions

---

## Future Enhancements

### Phase 2
- [ ] Azure STT adapter
- [ ] Prometheus metrics (latency, counts, errors)
- [ ] OpenTelemetry tracing

### Phase 3
- [ ] Dead-letter queue for failed publishes
- [ ] Retry logic with exponential backoff
- [ ] Circuit breaker for STT providers

### Phase 4
- [ ] Speaker diarization
- [ ] Multi-language support
- [ ] Custom vocabulary/phrases

---

## References

- [Google Cloud Speech-to-Text](https://cloud.google.com/speech-to-text/docs)
- [gRPC Streaming](https://grpc.io/docs/what-is-grpc/core-concepts/#server-streaming-rpc)
- [Kafka Go Client](https://github.com/segmentio/kafka-go)

