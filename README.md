# AI Speech Ingress Service

A Go gRPC service for real-time speech-to-text transcription. It receives audio streams, forwards them to an STT provider (Google Cloud Speech-to-Text), and publishes transcript events.

## Architecture

```
┌─────────────────┐     ┌─────────────────────┐     ┌─────────────────┐
│   gRPC Client   │────▶│ AI Speech Ingress   │────▶│  Google STT     │
│  (Audio Stream) │     │      Service        │     │                 │
└─────────────────┘     └─────────────────────┘     └─────────────────┘
                                │
                                ▼
                        ┌─────────────────┐
                        │  Event Publisher │
                        │  (Kafka/NATS)    │
                        └─────────────────┘
```

## Project Structure

```
ai-speech-ingress-service/
├── docker/
│   └── Dockerfile
├── helm/
│   └── ai-speech-ingress-service/
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/
├── proto/
│   └── audio.proto              # gRPC service definition
├── src/
│   ├── cmd/
│   │   ├── main.go              # Service entry point
│   │   └── testclient/          # Test client
│   ├── internal/
│   │   ├── api/grpc/            # gRPC server implementation
│   │   ├── config/              # Configuration
│   │   ├── events/              # Event publishing
│   │   ├── models/              # Data models
│   │   ├── schema/              # Validation
│   │   └── service/
│   │       ├── audio/           # Audio stream handler
│   │       ├── segment/         # Segment ID generator
│   │       ├── stt/             # STT adapter interface
│   │       │   └── google/      # Google STT implementation
│   │       └── transcription/   # Mock transcription (testing)
│   └── proto/                   # Generated protobuf code
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
| `GOOGLE_APPLICATION_CREDENTIALS` | Path to Google Cloud service account JSON | - |
| `KAFKA_ENABLED` | Enable Kafka publishing | `false` |
| `KAFKA_BROKERS` | Comma-separated Kafka broker addresses | `localhost:9092` |
| `KAFKA_TOPIC` | Kafka topic for transcript events | `interaction.transcripts` |
| `KAFKA_PRINCIPAL` | Principal name for event headers | `svc-speech-ingress` |

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

## Events

The service publishes transcript events:

### `interaction.transcript.partial`
```json
{
  "eventType": "interaction.transcript.partial",
  "interactionId": "int-123",
  "tenantId": "tenant-456",
  "segmentId": "int-123-seg-1",
  "text": "I want to cancel",
  "timestamp": 1736697600000
}
```

### `interaction.transcript.final`
```json
{
  "eventType": "interaction.transcript.final",
  "interactionId": "int-123",
  "tenantId": "tenant-456",
  "segmentId": "int-123-seg-1",
  "text": "I want to cancel my plan",
  "confidence": 0.94,
  "audioOffsetMs": 18420,
  "timestamp": 1736697600000
}
```

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
# Check Kafka offsets (message count)
kubectl exec -n infra k3a-kafka-0 -- /opt/kafka/bin/kafka-get-offsets.sh \
  --bootstrap-server localhost:9092 --topic interaction.transcripts

# List topics
kubectl exec -n infra k3a-kafka-0 -- /opt/kafka/bin/kafka-topics.sh \
  --bootstrap-server localhost:9092 --list

# Describe topic
kubectl exec -n infra k3a-kafka-0 -- /opt/kafka/bin/kafka-topics.sh \
  --bootstrap-server localhost:9092 --describe --topic interaction.transcripts
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
KAFKA_ENABLED=true KAFKA_BROKERS=localhost:9092 KAFKA_TOPIC=interaction.transcripts go run ./cmd

# Run with Google STT
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/credentials.json
go run ./cmd
```
