#!/bin/bash

# Build and test script for Go backend
# Run this before pushing to verify the build works

set -e

echo "=== Go Backend Build Validation ==="
echo ""

# 1. Check Go version
echo "1. Checking Go version..."
go version

# 2. Tidy dependencies
echo ""
echo "2. Tidying dependencies..."
go mod tidy

# 3. Build the application
echo ""
echo "3. Building application..."
go build -o /tmp/torrent-search-go ./main.go

# 4. Run tests (if any)
echo ""
echo "4. Running tests..."
go test ./... -v

# 5. Vet the code
echo ""
echo "5. Running go vet..."
go vet ./...

# 6. Check formatting
echo ""
echo "6. Checking code formatting..."
if [ -n "$(gofmt -l .)" ]; then
    echo "WARNING: Some files are not formatted correctly:"
    gofmt -l .
    echo "Run 'gofmt -w .' to fix formatting"
else
    echo "All files are properly formatted"
fi

# 7. Clean up
rm -f /tmp/torrent-search-go

echo ""
echo "=== Build validation complete! ==="
echo "The application builds successfully."
