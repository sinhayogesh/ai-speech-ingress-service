# 🎙️ Transcript Viewer

A real-time web UI for viewing transcription events from the AI Speech Ingress Service.

![Transcript Viewer](https://via.placeholder.com/800x500/0a0e14/00d4aa?text=Live+Transcript+Viewer)

## Features

- **Real-time updates** via WebSocket
- **Dual topic consumption** - Shows both partial and final transcripts
- **Segment tracking** - Groups transcripts by segment ID
- **Confidence display** - Visual confidence bar for final transcripts
- **Filtering** - Filter by interaction ID
- **Modern UI** - Dark theme with smooth animations

## Quick Start

### Prerequisites

- Go 1.24+
- Kafka running with transcript topics
- Port-forward to Kafka if in Kubernetes

### Run

```bash
# From the transcript-viewer directory
go mod tidy
go run main.go

# With custom options
go run main.go \
  -port=8081 \
  -brokers=localhost:9092 \
  -topic-partial=interaction.transcript.partial \
  -topic-final=interaction.transcript.final
```

### Access

Open http://localhost:8081 in your browser.

## Options

| Flag | Default | Description |
|------|---------|-------------|
| `-port` | `8081` | HTTP server port |
| `-brokers` | `localhost:9092` | Kafka brokers (comma-separated) |
| `-topic-partial` | `interaction.transcript.partial` | Partial transcript topic |
| `-topic-final` | `interaction.transcript.final` | Final transcript topic |

## Setup with Kubernetes

```bash
# Port-forward Kafka
kubectl port-forward -n infra svc/k3a-kafka 9092:9092 &

# Add hosts entry (one-time)
echo "127.0.0.1 k3a-kafka.infra.svc.cluster.local" | sudo tee -a /etc/hosts

# Run viewer
cd tools/transcript-viewer
go run main.go
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                  Transcript Viewer                       │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐  │
│  │   Kafka     │───▶│  Go Server  │───▶│  WebSocket  │  │
│  │  Consumer   │    │             │    │   Clients   │  │
│  └─────────────┘    └─────────────┘    └─────────────┘  │
│         │                                      │         │
│         │                                      │         │
│  ┌──────┴──────┐                      ┌───────┴───────┐ │
│  │   Topics    │                      │    Browser    │ │
│  │  - partial  │                      │   (HTML/JS)   │ │
│  │  - final    │                      └───────────────┘ │
│  └─────────────┘                                        │
│                                                          │
└─────────────────────────────────────────────────────────┘
```

## UI Features

### Stats Bar
- **Partials** - Count of partial transcripts received
- **Finals** - Count of final transcripts received
- **Segments** - Number of unique segments

### Transcript Panel
- Color-coded entries (purple = partial, green = final)
- Timestamp and segment ID
- Confidence bar for finals
- Auto-scroll to latest

### Segments Panel
- Lists all segments
- Shows partial count and final status
- Click to filter transcripts

## Development

```bash
# Install dependencies
go mod tidy

# Run with hot reload (using air)
air

# Build binary
go build -o transcript-viewer
```

