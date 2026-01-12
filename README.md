# ai-speech-ingress-service

A Go service for AI speech ingress.

## Project Structure

```
ai-speech-ingress-service/
├── docker/
│   └── Dockerfile           # Docker build configuration
├── helm/
│   └── ai-speech-ingress-service/
│       ├── Chart.yaml       # Helm chart metadata
│       ├── values.yaml      # Default configuration values
│       └── templates/
│           ├── deployment.yaml
│           └── service.yaml
├── src/
│   ├── cmd/
│   │   └── main.go          # Application entry point
│   ├── internal/
│   │   ├── app/
│   │   │   └── app.go       # Application state and lifecycle
│   │   ├── config/
│   │   │   └── config.go    # Configuration loading
│   │   └── http/
│   │       └── router.go    # HTTP routing
│   ├── go.mod
│   └── go.sum
├── Tiltfile                  # Local development with Tilt
└── README.md
```

## Development

### Prerequisites

- Go 1.24.2+
- Docker
- Kubernetes cluster (for deployment)
- Tilt (for local development)

### Running Locally

```bash
cd src
go mod download
go run ./cmd
```

### Building

```bash
cd src
go build -o ai-speech-ingress-service ./cmd
```

### Local Development with Tilt

Ensure your environment has `LOCAL_REGISTRY` set, then:

```bash
tilt up
```

## Configuration

The service is configured via environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `HTTP_LISTEN_ADDRESS` | Main HTTP server address | `:8080` |
| `HEALTH_LISTEN_ADDRESS` | Health check server address | `:8081` |
| `ZEROLOG_LOG_LEVEL` | Log level (debug, info, warn, error) | `info` |
| `ENV` | Environment (dev, prod) | - |

## API Endpoints

### Health Checks

- `GET /v1/liveness` - Liveness probe
- `GET /v1/readiness` - Readiness probe

## Deployment

The service is deployed via Helm:

```bash
helm install ai-speech-ingress-service ./helm/ai-speech-ingress-service
```
