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
| Google | `stt/google/adapter.go` | Uses `SingleUtterance` mode, configurable params |

**Google STT Configuration:**

The Google adapter accepts configuration via `STT_*` environment variables:

```go
cfg := google.Config{
    LanguageCode:    "en-US",       // STT_LANGUAGE_CODE
    SampleRateHz:    8000,          // STT_SAMPLE_RATE_HZ
    InterimResults:  true,          // STT_INTERIM_RESULTS
    AudioEncoding:   "LINEAR16",    // STT_AUDIO_ENCODING
    SingleUtterance: false,         // STT_SINGLE_UTTERANCE (default: continuous mode)
}
adapter, err := google.NewWithConfig(ctx, cfg)
```

**Transcription Modes:**

| Mode | `SingleUtterance` | Behavior |
|------|-------------------|----------|
| **Continuous** (default) | `false` | Multiple finals per session, each creates new segment |
| **Single Utterance** | `true` | Session restarts after each detected utterance |

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

### Solution: Two Modes

The service supports two transcription modes, configurable via `STT_SINGLE_UTTERANCE`:

#### Continuous Mode (Default, `STT_SINGLE_UTTERANCE=false`)

In continuous mode, Google transcribes the entire audio stream in a single session:

```
Audio: "Sentence 1" "Sentence 2" "Sentence 3" ... "Sentence 10"
                │           │           │               │
                ▼           ▼           ▼               ▼
            seg-1       seg-2       seg-3    ...    seg-10
         (isFinal)   (isFinal)   (isFinal)       (isFinal)
```

**How it works:**
1. Google sends `isFinal=true` for each detected sentence
2. Handler creates new segment after each final (no session restart)
3. All audio is processed in one continuous session
4. Best for: recordings, multi-sentence audio

**Handler Behavior (Continuous Mode):**
```go
// When final is received in continuous mode:
1. Emit final transcript for current segment
2. Close current segment (FINAL_EMITTED → CLOSED)
3. Generate new segmentId
4. Reset segment metrics
5. Continue receiving from same STT session
```

#### Single Utterance Mode (`STT_SINGLE_UTTERANCE=true`)

In single utterance mode, Google stops after each detected speech pause:

```
Audio: "Hello" [pause] "How are you" [pause] ...
            │               │
            ▼               ▼
    END_OF_SINGLE_UTTERANCE + final
            │               │
       seg-1 complete   seg-2 complete
       [restart STT]    [restart STT]
```

**How it works:**
1. Google detects end-of-speech via Voice Activity Detection (VAD)
2. Returns `END_OF_SINGLE_UTTERANCE` event followed by final
3. Handler restarts STT session for next utterance
4. Best for: conversational audio with natural pauses

**Caveat:** When restarting, any audio sent to the old session but not yet transcribed may be lost. This can cause missed utterances in rapidly-spoken audio.

**Handler Behavior (Single Utterance Mode):**
```go
// When END_OF_SINGLE_UTTERANCE is received:
1. Set pendingRestart flag
// When final is received after END_OF_SINGLE_UTTERANCE:
2. Emit final transcript
3. Close current segment
4. Generate new segmentId
5. Restart STT adapter (new session)
6. Start new Listen goroutine
```

#### Mock STT

Simulates utterance completion:
- After all partials sent (based on frame count)
- Sends final + `OnEndOfUtterance()`

### Mode Selection

| Scenario | Recommended Mode | Env Var |
|----------|------------------|---------|
| Multi-sentence recordings | Continuous | `STT_SINGLE_UTTERANCE=false` |
| Call center transcription | Continuous | `STT_SINGLE_UTTERANCE=false` |
| Voice commands | Single Utterance | `STT_SINGLE_UTTERANCE=true` |
| Conversational with pauses | Single Utterance | `STT_SINGLE_UTTERANCE=true` |

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

## Configuration

All behavior is adjustable via environment variables with safe defaults. No dynamic reloads. No config sprawl.

### Design Principles

1. **Safe defaults** - Service runs correctly out-of-the-box
2. **Externalized** - All tunable parameters via env vars
3. **No reloads** - Restart required for config changes (simplicity > complexity)
4. **Future-ready** - STT params will vary per tenant/region later

### Configuration Categories

#### A) Service Identity & Runtime

| Config | Default | Env Var | Purpose |
|--------|---------|---------|---------|
| Service Principal | `svc-speech-ingress` | `SERVICE_PRINCIPAL` | Auth/authz identity |
| gRPC Port | `50051` | `GRPC_PORT` | Service listen port |
| Log Level | `info` | `LOG_LEVEL` | Logging verbosity |

#### B) STT Parameters

| Config | Default | Env Var | Purpose |
|--------|---------|---------|---------|
| Provider | `mock` | `STT_PROVIDER` | STT backend (`mock`, `google`) |
| Language Code | `en-US` | `STT_LANGUAGE_CODE` | BCP-47 transcription language |
| Sample Rate | `8000` | `STT_SAMPLE_RATE_HZ` | Audio sample rate (Hz) |
| Interim Results | `true` | `STT_INTERIM_RESULTS` | Enable partial transcripts |
| Audio Encoding | `LINEAR16` | `STT_AUDIO_ENCODING` | Audio format |
| Single Utterance | `false` | `STT_SINGLE_UTTERANCE` | Utterance mode (see below) |

**Supported encodings:** `LINEAR16`, `MULAW`, `FLAC`, `AMR`, `AMR_WB`, `OGG_OPUS`, `SPEEX_WITH_HEADER_BYTE`, `WEBM_OPUS`

**Single Utterance Mode:**
- `false` (default): **Continuous mode** - Google sends multiple finals per session, best for multi-sentence audio
- `true`: **Single utterance mode** - Session restarts after each utterance, best for conversational with pauses

#### C) Segment Guardrails

| Config | Default | Env Var | Purpose |
|--------|---------|---------|---------|
| Max Audio Bytes | 5MB | `SEGMENT_MAX_AUDIO_BYTES` | Memory exhaustion prevention |
| Max Duration | 5m | `SEGMENT_MAX_DURATION` | Zombie segment prevention |
| Max Partials | 500 | `SEGMENT_MAX_PARTIALS` | Downstream flood prevention |

### Loading

Configuration is loaded once at startup from `internal/config/config.go`:

```go
cfg := config.Load()

// Service identity
cfg.Service.Principal  // "svc-speech-ingress"
cfg.Service.GRPCPort   // "50051"

// STT parameters
cfg.STT.Provider        // "mock"
cfg.STT.LanguageCode    // "en-US"
cfg.STT.SampleRateHz    // 8000
cfg.STT.InterimResults  // true
cfg.STT.AudioEncoding   // "LINEAR16"
cfg.STT.SingleUtterance // false (continuous mode by default)

// Segment limits
cfg.SegmentLimits.MaxAudioBytes // 5MB
cfg.SegmentLimits.MaxDuration   // 5m
cfg.SegmentLimits.MaxPartials   // 500
```

### Startup Logging

All configuration is logged at startup for visibility:

```json
{"level":"info","servicePrincipal":"svc-speech-ingress","grpcPort":"50051","logLevel":"info","message":"Starting Speech Ingress Service"}
{"level":"info","provider":"google","languageCode":"en-US","sampleRateHz":8000,"interimResults":true,"audioEncoding":"LINEAR16","singleUtterance":false,"message":"STT configuration"}
{"level":"info","maxAudioBytes":5242880,"maxDuration":"5m0s","maxPartials":500,"message":"Segment limits"}
```

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

### Override Example

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

