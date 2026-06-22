#!/bin/bash

# Setup script for torrent-search-go
# This script installs dependencies and configures the development environment

set -e

echo "=== Torrent Search Go Setup ==="
echo ""

# Check Go version
echo "Checking Go version..."
go version
GO_VERSION=$(go version | awk '{print $3}')
echo "Go version: $GO_VERSION"
echo ""

# Download dependencies
echo "Downloading Go dependencies..."
go mod download
echo ""

# Install development tools
echo "Installing development tools..."

# golangci-lint
if ! command -v golangci-lint &> /dev/null; then
    echo "Installing golangci-lint..."
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
fi

# mockgen
if ! command -v mockgen &> /dev/null; then
    echo "Installing mockgen..."
    go install github.com/golang/mock/mockgen@latest
fi

# errcheck
if ! command -v errcheck &> /dev/null; then
    echo "Installing errcheck..."
    go install github.com/kisielk/errcheck@latest
fi

# govulncheck
if ! command -v govulncheck &> /dev/null; then
    echo "Installing govulncheck..."
    go install golang.org/x/vuln/cmd/govulncheck@latest
fi

echo ""


# Setup Caveman
echo "Caveman configuration is in .caveman/config.yaml"
echo "Run 'caveman generate --package <package>' to generate boilerplate"
echo ""

# Install pre-commit hooks
echo "Setting up pre-commit hooks..."
if command -v pre-commit &> /dev/null; then
    pre-commit install
    echo "Pre-commit hooks installed"
else
    echo "pre-commit not found. Install with: pip install pre-commit"
fi
echo ""

# Run initial lint
echo "Running initial lint..."
golangci-lint run || true
echo ""

# Run tests
echo "Running tests..."
go test ./... || true
echo ""

echo "=== Setup Complete ==="
echo ""
echo "Next steps:"
echo "1. Copy .env.example to .env and configure your environment"
echo "2. Run 'go run ./cmd/server' to start the development server"
echo "3. Visit http://localhost:3001/health to verify the server is running"
