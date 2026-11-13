# syntax=docker/dockerfile:1

# ==============================================================================
# Stage 1: Build GonDesk Binary (Pure Go, CGO_ENABLED=0)
# ==============================================================================
FROM golang:1.24-alpine AS builder

WORKDIR /src

# Enable Go toolchain auto-upgrade so Go 1.27.3+ required by MyGo is fetched automatically
ENV GOTOOLCHAIN=auto
ENV CGO_ENABLED=0

RUN apk add --no-cache git ca-certificates tzdata

# Cache dependencies first
COPY go.mod go.sum ./
RUN go mod download

# Copy application source
COPY . .

# Build static binary for the target platform
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=0.1.0

RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/gondesk .

# ==============================================================================
# Stage 2: Minimal Production Runtime (Global Bridge & TURN Server Mode)
# ==============================================================================
FROM alpine:3.21

LABEL org.opencontainers.image.title="GonDesk" \
      org.opencontainers.image.description="Single-Port Remote Desktop & Global HTTPS/WSS + Pion TURN Bridge Server" \
      org.opencontainers.image.licenses="MIT"

RUN apk add --no-cache ca-certificates tzdata curl \
    && adduser -D -u 10001 gondesk \
    && mkdir -p /data/certs \
    && chown -R gondesk:gondesk /data

WORKDIR /data

COPY --from=builder /out/gondesk /usr/local/bin/gondesk

USER gondesk

# Environment defaults for Desktop Server (Global Bridge) headless mode
ENV GONDESK_MODE=server \
    GONDESK_HEADLESS=true \
    GONDESK_HOST=0.0.0.0 \
    GONDESK_PORT=8443 \
    GONDESK_TLS=true \
    GONDESK_DATA_DIR=/data

# Single Port for HTTPS (GoFiber v2), WSS Signaling, TURNS (TLS/TCP), and TURN/STUN (UDP)
EXPOSE 8443/tcp
EXPOSE 8443/udp

VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -kfsS https://127.0.0.1:${GONDESK_PORT}/api/v1/info || exit 1

ENTRYPOINT ["/usr/local/bin/gondesk"]
CMD ["--mode=server", "--headless", "--host=0.0.0.0", "--port=8443", "--data-dir=/data"]

