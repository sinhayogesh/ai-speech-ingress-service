.DEFAULT_GOAL := help

help: ## Display this help message
	@echo "Available commands:"
	@awk 'BEGIN {FS = ":.*## "; printf "\n%-20s %s\n\n", "Target", "Description"} /^[a-zA-Z_-]+:.*?## / {printf "%-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# ---------------------------------------------------------
# Protobuf
# ---------------------------------------------------------

proto: ## Generate Go code from protobuf definitions
	protoc --go_out=. --go-grpc_out=. proto/audio.proto

proto-install: ## Install protoc plugins for Go
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# ---------------------------------------------------------
# Build
# ---------------------------------------------------------

build: ## Build the service binary
	cd src && go build -o ../bin/ai-speech-ingress-service ./cmd

run: ## Run the service locally
	cd src && ENV=dev go run ./cmd

test: ## Run tests
	cd src && go test -v ./...

test-client: ## Run the test gRPC client
	cd src && go run ./cmd/testclient

audio-test: ## Stream real audio file to service (requires running server)
	cd src && go run ./cmd/audioclient -audio=../testdata/sample-8khz.wav

transcript-viewer: ## Run the real-time transcript viewer UI
	cd tools/transcript-viewer && go run main.go

# ---------------------------------------------------------
# Dependencies
# ---------------------------------------------------------

deps: ## Download Go dependencies
	cd src && go mod download

tidy: ## Tidy Go modules
	cd src && go mod tidy

# ---------------------------------------------------------
# Docker
# ---------------------------------------------------------

docker-build: ## Build Docker image
	docker build -f docker/Dockerfile -t ai-speech-ingress-service:latest .

# ---------------------------------------------------------
# Clean
# ---------------------------------------------------------

clean: ## Clean build artifacts
	rm -rf bin/

