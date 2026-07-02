# Build stage
FROM golang:1.25-alpine AS builder

# Install necessary build dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /build/server .

# Build the Next.js dashboard static export (served by the Go binary at /).
FROM node:20-alpine AS web-builder
WORKDIR /web
COPY dashboard/package*.json ./
RUN npm ci
COPY dashboard/ ./
RUN npm run build          # -> /web/out (index.html + _next/static/*)

# Runtime stage
FROM alpine:3.19

# Install dumb-init for proper signal handling
RUN apk add --no-cache dumb-init tzdata wget

# Create non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -S appuser -u 1001 -G appgroup

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder --chown=appuser:appgroup /build/server /app/server

# Copy the Next.js dashboard static export (built by web-builder).
COPY --from=web-builder --chown=appuser:appgroup /web/out /app/static/dashboard

# Create necessary directories
RUN mkdir -p /app/logs && chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

ENV HOST=0.0.0.0
ENV PORT=8080
ENV LOG_DIR=/app/logs

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD sh -c 'wget --no-verbose --tries=1 --spider "http://127.0.0.1:${PORT:-8080}/health" || exit 1'

# Label for deployment
LABEL org.opencontainers.image.source=https://github.com/akshatsinghkaushik/torrent-search-go

# Use dumb-init to handle signals properly
ENTRYPOINT ["dumb-init", "--"]

# Start the application
CMD ["./server"]
