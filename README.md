# AI Speech Ingress Service

A Go gRPC service for real-time speech-to-text transcription. It receives audio streams via gRPC, forwards them to configurable STT providers, detects utterance boundaries, and publishes transcript events to Kafka.

## Features

- **Real-time streaming** - Bidirectional gRPC for low-latency audio processing
- **Multi-provider STT** - Pluggable adapters (Google, Mock; Azure planned)
- **Utterance boundary detection** - Automatic segment transitions on speech pauses
- **Separate Kafka topics** - Infrastructure-level access control for partials vs finals
- **Kubernetes-native** - Helm charts, health probes, graceful shutdown

## Architecture

```
                                    ┌─────────────────────────────────────────┐
                                    │       AI Speech Ingress Service         │
                                    │                                         │
┌──────────────┐    gRPC Stream     │  ┌─────────────┐    ┌───────────────┐  │
│  Telephony   │───────────────────▶│  │   Audio     │───▶│  STT Adapter  │  │
│   Gateway    │   AudioFrame[]     │  │  Handler    │    │  (Google/Mock)│  │
└──────────────┘                    │  └─────────────┘    └───────────────┘  │
                                    │         │                   │           │
                                    │         │ OnPartial()       │           │
                                    │         │ OnFinal()         │           │
                                    │         │ OnEndOfUtterance()│           │
                                    │         ▼                   │           │
                                    │  ┌─────────────┐            │           │
                                    │  │  Segment    │◀───────────┘           │
                                    │  │  Generator  │                        │
                                    │  └─────────────┘                        │
                                    │         │                               │
                                    │         ▼                               │
                                    │  ┌─────────────┐                        │
                                    │  │  Publisher  │                        │
                                    │  └─────────────┘                        │
                                    └─────────┬───────────────────────────────┘
                                              │
                         ┌────────────────────┴────────────────────┐
                         │                                         │
                         ▼                                         ▼
              ┌─────────────────────┐                 ┌─────────────────────┐
              │ interaction.        │                 │ interaction.        │
              │ transcript.partial  │                 │ transcript.final    │
              │     (Kafka)         │                 │     (Kafka)         │
              └─────────────────────┘                 └─────────────────────┘
```

## Component Design

### STT Adapter Interface

```go
type Callback interface {
    OnPartial(text string)                    // Interim transcription result
    OnFinal(text string, confidence float64)  // Final confirmed result
    OnEndOfUtterance()                        // Speaker stopped talking
    OnError(err error)                        // Transcription error
}

type Adapter interface {
    Start(ctx context.Context, cb Callback) error  // Begin session
    SendAudio(ctx context.Context, audio []byte) error  // Stream audio
    Close() error  // End session
}
```

### Supported Providers

| Provider | Status | Notes |
|----------|--------|-------|
| **Mock** | ✅ Ready | Simulates realistic transcription for testing |
| **Google STT** | ✅ Ready | Uses `SingleUtterance` mode for boundary detection |
| **Azure STT** | 🔜 Planned | Future implementation |
| **AWS Transcribe** | 🔜 Planned | Future implementation |

### Audio Handler

Coordinates between STT adapter and event publishing:
- Implements `stt.Callback` to receive transcripts
- Manages segment lifecycle (create, transition, finalize)
- Publishes events to appropriate Kafka topics

### Segment Generator

Thread-safe generator for unique segment IDs:
- Format: `{interactionId}-seg-{n}`
- Atomic counter ensures uniqueness
- Resets per interaction

> 📖 **For detailed technical documentation, see [docs/DESIGN.md](docs/DESIGN.md)**

## Project Structure

```
ai-speech-ingress-service/
├── docker/
│   └── Dockerfile              # Multi-stage build
├── helm/
│   └── ai-speech-ingress-service/
│       ├── Chart.yaml
│       ├── values.yaml          # Kafka, STT configuration
│       └── templates/
│           ├── deployment.yaml  # gRPC health probes
│           └── service.yaml
├── k8s/
│   └── kafka-ui.yaml           # Kafka UI deployment
├── proto/
│   └── audio.proto             # gRPC service definition
├── src/
│   ├── cmd/
│   │   ├── main.go             # Service entry point
│   │   └── testclient/         # gRPC test client
│   ├── internal/
│   │   ├── api/grpc/           # gRPC server (StreamAudio)
│   │   ├── config/             # Environment configuration
│   │   ├── events/             # Kafka publisher (dual topics)
│   │   ├── models/             # TranscriptPartial, TranscriptFinal
│   │   ├── schema/             # Validation (stub)
│   │   └── service/
│   │       ├── audio/          # Audio handler + segment transitions
│   │       ├── segment/        # Thread-safe segment ID generator
│   │       └── stt/
│   │           ├── adapter.go  # Adapter + Callback interfaces
│   │           ├── google/     # Google Cloud STT adapter
│   │           └── mock/       # Mock adapter for testing
│   └── proto/                  # Generated protobuf code
├── Makefile
├── Tiltfile
└── README.md
```

## Quick Start

### Prerequisites

- Go 1.24+
- Google Cloud credentials (for STT)
- Docker (for deployment)

### Run Locally

```bash
# Set Google credentials
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json

# Run the service
make run
```

### Test with Client

```bash
# In another terminal
make test-client
```

## Configuration

| Environment Variable | Description | Default |
|---------------------|-------------|---------|
| `GRPC_PORT` | gRPC server port | `50051` |
| `STT_PROVIDER` | STT provider (`mock`, `google`) | `mock` |
| `GOOGLE_APPLICATION_CREDENTIALS` | Path to Google Cloud service account JSON | - |
| `KAFKA_ENABLED` | Enable Kafka publishing | `false` |
| `KAFKA_BROKERS` | Comma-separated Kafka broker addresses | `localhost:9092` |
| `KAFKA_TOPIC_PARTIAL` | Kafka topic for partial transcript events | `interaction.transcript.partial` |
| `KAFKA_TOPIC_FINAL` | Kafka topic for final transcript events | `interaction.transcript.final` |
| `KAFKA_PRINCIPAL` | Principal name for event headers | `svc-speech-ingress` |

### STT Provider Selection

```bash
# Use mock STT (default, no cloud credentials needed)
STT_PROVIDER=mock go run ./cmd

# Use Google Cloud STT (requires credentials)
STT_PROVIDER=google \
GOOGLE_APPLICATION_CREDENTIALS=/path/to/creds.json \
go run ./cmd
```

## gRPC API

### `StreamAudio`

Client-streaming RPC for audio transcription.

**Request (`AudioFrame`):**
- `interactionId` - Unique interaction identifier
- `tenantId` - Tenant identifier
- `audio` - Raw audio bytes
- `audioOffsetMs` - Audio offset in milliseconds
- `endOfUtterance` - Signals end of speech

**Response (`StreamAck`):**
- `interactionId` - Confirmed interaction ID

## Data Model

### Hierarchy

```
interactionId (conversation / call)
  │
  ├── segmentId = seg-1 (utterance #1)
  │     ├── transcript.partial  → "I want"
  │     ├── transcript.partial  → "I want to cancel"
  │     └── transcript.final    → "I want to cancel my subscription" (exactly once)
  │
  ├── segmentId = seg-2 (utterance #2)
  │     ├── transcript.partial  → "Yes"
  │     └── transcript.final    → "Yes please go ahead"
  │
  └── segmentId = seg-3 (utterance #3)
        ├── transcript.partial  → "Thank"
        ├── transcript.partial  → "Thank you"
        └── transcript.final    → "Thank you very much"
```

### Key Concepts

| Concept | Description | Example |
|---------|-------------|---------|
| **interactionId** | Unique identifier for a conversation/call. Persists for the entire call duration. | `call-abc-123` |
| **segmentId** | Unique identifier for an utterance within a call. Auto-generated as `{interactionId}-seg-{n}`. | `call-abc-123-seg-1` |
| **Partial Transcript** | Interim result as speech is being processed. Multiple per segment. Low latency, may change. | "I want to can" |
| **Final Transcript** | Confirmed result after utterance ends. **Exactly one per segment**. Higher accuracy. | "I want to cancel" |

### Guarantees

- Each `segmentId` will have **zero or more** partial transcripts
- Each `segmentId` will have **exactly one** final transcript
- Partials are ordered by timestamp within a segment
- The final transcript is always the last event for a segment
- Separate Kafka topics enable independent ACLs and consumer groups

### Utterance Boundary Detection

The service detects when a speaker stops talking (utterance boundary) and automatically:

1. **Sends final transcript** for the completed utterance
2. **Generates new segmentId** for the next utterance
3. **Continues processing** subsequent speech in the new segment

```
Audio Stream: "I want to cancel" [pause] "Yes please go ahead"
                     │                         │
                     ▼                         ▼
              ┌─────────────┐           ┌─────────────┐
              │   seg-1     │           │   seg-2     │
              │ "I want to  │           │ "Yes please │
              │  cancel"    │           │  go ahead"  │
              └─────────────┘           └─────────────┘
                     │                         │
            End of Utterance          End of Utterance
                 Detected                  Detected
```

**How it works:**
- **Google STT**: Uses `SingleUtterance` mode which returns `END_OF_SINGLE_UTTERANCE` event
- **Mock STT**: Simulates utterance completion after all partials are sent
- **Handler**: Transitions to new segment on `OnEndOfUtterance()` callback

## Events

The service publishes transcript events to **separate Kafka topics** for infrastructure-level access control:

### `interaction.transcript.partial` (Topic: `interaction.transcript.partial`)

Published for each interim transcription result. Multiple events per segment.

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

| Field | Type | Description |
|-------|------|-------------|
| `eventType` | string | Always `interaction.transcript.partial` |
| `interactionId` | string | Conversation/call identifier |
| `tenantId` | string | Tenant identifier |
| `segmentId` | string | Utterance identifier (unique per segment) |
| `text` | string | Current interim transcript text |
| `timestamp` | int64 | Event timestamp (Unix ms) |

### `interaction.transcript.final` (Topic: `interaction.transcript.final`)

Published exactly once per segment when the utterance ends.

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

| Field | Type | Description |
|-------|------|-------------|
| `eventType` | string | Always `interaction.transcript.final` |
| `interactionId` | string | Conversation/call identifier |
| `tenantId` | string | Tenant identifier |
| `segmentId` | string | Utterance identifier (unique per segment) |
| `text` | string | Final confirmed transcript text |
| `confidence` | float64 | STT confidence score (0.0 - 1.0) |
| `audioOffsetMs` | int64 | Audio offset when utterance ended |
| `timestamp` | int64 | Event timestamp (Unix ms) |

## Make Targets

| Target | Description |
|--------|-------------|
| `make run` | Run the service locally |
| `make test-client` | Run the test gRPC client |
| `make build` | Build the service binary |
| `make proto` | Generate protobuf code |
| `make proto-install` | Install protoc plugins |
| `make docker-build` | Build Docker image |
| `make test` | Run tests |

## Deployment

### Kubernetes (Helm)

```bash
helm install ai-speech-ingress-service ./helm/ai-speech-ingress-service
```

### Local Lab (Tilt)

```bash
cd ../axp-local-lab
make run-ai-speech-ingress-service
```

## Quick Reference Commands

### Deploy & Test

```bash
# Deploy to local lab
cd ~/Work/25.4/axp-local-lab
make run-ai-speech-ingress-service

# Port-forward the gRPC service
kubectl port-forward -n core deploy/ai-speech-ingress-service 50051:50051 &

# Run test client
cd ~/Work/25.4/ai-speech-ingress-service
make test-client
```

### Kafka

```bash
# Check Kafka offsets (message count) - separate topics
kubectl exec -n infra k3a-kafka-0 -- /opt/kafka/bin/kafka-get-offsets.sh \
  --bootstrap-server localhost:9092 --topic interaction.transcript.partial

kubectl exec -n infra k3a-kafka-0 -- /opt/kafka/bin/kafka-get-offsets.sh \
  --bootstrap-server localhost:9092 --topic interaction.transcript.final

# List topics
kubectl exec -n infra k3a-kafka-0 -- /opt/kafka/bin/kafka-topics.sh \
  --bootstrap-server localhost:9092 --list | grep interaction

# Describe topics
kubectl exec -n infra k3a-kafka-0 -- /opt/kafka/bin/kafka-topics.sh \
  --bootstrap-server localhost:9092 --describe --topic interaction.transcript.partial

kubectl exec -n infra k3a-kafka-0 -- /opt/kafka/bin/kafka-topics.sh \
  --bootstrap-server localhost:9092 --describe --topic interaction.transcript.final
```

### Kafka UI

```bash
# Deploy Kafka UI (one-time)
kubectl apply -f k8s/kafka-ui.yaml

# Port-forward Kafka UI
kubectl port-forward -n infra svc/kafka-ui 8080:8080 &

# Open in browser: http://localhost:8080
```

### Debugging

```bash
# View pod logs
kubectl logs -n core deploy/ai-speech-ingress-service --tail=50 -f

# Check pod status
kubectl get pods -n core | grep ai-speech

# Restart deployment
kubectl rollout restart -n core deploy/ai-speech-ingress-service

# Kill port-forward on specific port
lsof -ti:50051 | xargs kill -9

# gRPC health check
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check

# List gRPC services (requires reflection)
grpcurl -plaintext localhost:50051 list
```

### Local Development (without Kubernetes)

```bash
# Run locally with mock STT
cd src && go run ./cmd

# Run locally with Kafka (requires port-forward to Kafka)
kubectl port-forward -n infra svc/k3a-kafka 9092:9092 &
echo "127.0.0.1 k3a-kafka.infra.svc.cluster.local" | sudo tee -a /etc/hosts
KAFKA_ENABLED=true KAFKA_BROKERS=localhost:9092 \
  KAFKA_TOPIC_PARTIAL=interaction.transcript.partial \
  KAFKA_TOPIC_FINAL=interaction.transcript.final \
  go run ./cmd

# Run with Google STT
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/credentials.json
go run ./cmd
```
