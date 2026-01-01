# ==============================================================================
# backend/Dockerfile
# Multi-stage build for Golang backend API
# ==============================================================================

# Stage 1: Builder
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.Version=${VERSION:-dev}" \
    -o /build/server \
    ./cmd/server

# Build migration tool
# Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7, 11.8
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.Version=${VERSION:-dev}" \
    -o /build/migrate \
    ./cmd/migrate

# Stage 2: Runtime
FROM alpine:3.19 AS runtime

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Set timezone
ENV TZ=Asia/Jakarta

# Create non-root user
RUN addgroup -g 1000 app && \
    adduser -u 1000 -G app -s /bin/sh -D app

# Create necessary directories
RUN mkdir -p /app/migrations && chown -R app:app /app

WORKDIR /app

# Copy binaries from builder
COPY --from=builder /build/server /app/server
COPY --from=builder /build/migrate /app/migrate
COPY --from=builder /build/migrations /app/migrations

# Switch to non-root user
USER app

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the server
ENTRYPOINT ["/app/server"]
