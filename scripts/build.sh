#!/bin/bash

set -e

# Get script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/.." && pwd )"

# Create bin directory if it doesn't exist
mkdir -p "$PROJECT_ROOT/bin"

# Change to src directory where go.mod is located
cd "$PROJECT_ROOT/src"

# Collect build metadata
VERSION="${VERSION:-dev}"
COMMIT_HASH=$(git -C "$PROJECT_ROOT" rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u '+%Y-%m-%dT%H:%M:%SZ')

LDFLAGS="-w -s -X main.Version=${VERSION} -X main.CommitHash=${COMMIT_HASH} -X main.BuildTime=${BUILD_TIME}"

# Build main API for multiple platforms
# CGO_ENABLED=0 ensures static linking (required for distroless/alpine containers)
echo "Building API binaries (version=${VERSION} commit=${COMMIT_HASH})..."

# Build for Linux AMD64
echo "Building for linux/amd64..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$LDFLAGS" -o ../bin/clamav-api-linux-amd64 .

# Build for Linux ARM64
echo "Building for linux/arm64..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$LDFLAGS" -o ../bin/clamav-api-linux-arm64 .

# Build for macOS AMD64
echo "Building for darwin/amd64..."
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="$LDFLAGS" -o ../bin/clamav-api-darwin-amd64 .

# Build for macOS ARM64 (Apple Silicon)
echo "Building for darwin/arm64..."
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$LDFLAGS" -o ../bin/clamav-api-darwin-arm64 .

# Build for Windows AMD64
echo "Building for windows/amd64..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$LDFLAGS" -o ../bin/clamav-api-windows-amd64.exe .

# Make binaries executable
chmod +x "$PROJECT_ROOT/bin"/*

echo ""
echo "Build completed successfully!"
echo "Binaries available in ./bin/"
echo ""
ls -lh "$PROJECT_ROOT/bin/"
