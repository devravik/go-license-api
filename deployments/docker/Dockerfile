# deployments/docker/Dockerfile

# ── Stage 1: Build ──────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Download modules first (layer cached unless go.mod changes)
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a static binary with stripped symbols for smaller size
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
  -ldflags="-s -w" \
  -o /app/server ./cmd/server/main.go

# ── Stage 2: Runtime ─────────────────────────────────────────────────────────
# Use distroless for minimal attack surface
FROM gcr.io/distroless/static-debian12

WORKDIR /app

# Only copy the compiled binary — no Go toolchain, no source code
COPY --from=builder /app/server .

# Use non-root user (distroless uses uid 65532 by default)
USER 65532:65532

EXPOSE 3000

ENTRYPOINT ["/app/server"]
