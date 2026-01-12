FROM acraocpshsrvnonprod.azurecr.io/infinity/go-builder:latest AS builder

WORKDIR /app/ai-speech-ingress-service

# Install git so Go can fetch modules hosted in git repos.
RUN dnf install -y git && dnf clean all

# Copy Go module files first to leverage Docker layer caching for dependencies.
COPY src/go.mod src/go.sum ./src/
RUN cd src && go mod download

# Copy the full source tree.
COPY src ./src

WORKDIR /app/ai-speech-ingress-service/src

ARG ARCH
ENV M_ARCH=${ARCH}

# Build the service binary for the target architecture.
RUN --mount=type=cache,target=/root/.cache/go-build if [ "$ARCH" = "aarch64" ]; then \
      CGO_ENABLED=0 GOARCH=arm64 GOOS=linux go build -ldflags="-s -w" -o /app/ai-speech-ingress-service/ai-speech-ingress-service ./cmd; \
    else \
      CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build -ldflags="-s -w" -o /app/ai-speech-ingress-service/ai-speech-ingress-service ./cmd; \
    fi

FROM acraocpshsrvnonprod.azurecr.io/infinity/go-runtime:latest AS final

# Ensure internal certificates are trusted
RUN cp /opt/avaya/ca/zscaler.crt /etc/pki/ca-trust/source/anchors/ && update-ca-trust extract

LABEL maintainer="Avaya, Inc." \
  team="Infinity" \
  description="AI Speech Ingress service" \
  org.opencontainers.image.source="https://github.com/Solutions-and-Technology/ai-speech-ingress-service"

ENV APP_NAME=ai-speech-ingress-service

WORKDIR /app

COPY --from=builder /app/ai-speech-ingress-service/ai-speech-ingress-service /app/ai-speech-ingress-service

EXPOSE 8080

ENTRYPOINT ["run-application", "--application-type", "go", "--fips-env", "GODEBUG=fips140=on", "--"]
CMD ["/app/ai-speech-ingress-service"]

