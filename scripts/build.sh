#!/bin/bash

set -e

# Get script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/.." && pwd )"

# Create bin directory if it doesn't exist
mkdir -p "$PROJECT_ROOT/bin"

# Change to src directory where go.mod is located
cd "$PROJECT_ROOT/src"

# Build main API for multiple platforms
# CGO_ENABLED=0 ensures static linking (required for distroless/alpine containers)
echo "Building API binaries..."

# Build for Linux AMD64
echo "Building for linux/amd64..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o ../bin/clamav-api-linux-amd64 main.go grpc_server.go

# Build for Linux ARM64
echo "Building for linux/arm64..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-w -s" -o ../bin/clamav-api-linux-arm64 main.go grpc_server.go

# Build for macOS AMD64
echo "Building for darwin/amd64..."
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="-w -s" -o ../bin/clamav-api-darwin-amd64 main.go grpc_server.go

# Build for macOS ARM64 (Apple Silicon)
echo "Building for darwin/arm64..."
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-w -s" -o ../bin/clamav-api-darwin-arm64 main.go grpc_server.go

# Build for Windows AMD64
echo "Building for windows/amd64..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-w -s" -o ../bin/clamav-api-windows-amd64.exe main.go grpc_server.go

# Make binaries executable
chmod +x "$PROJECT_ROOT/bin"/*

echo ""
echo "✅ Build completed successfully!"
echo "📦 Binaries available in ./bin/"
echo ""
ls -lh "$PROJECT_ROOT/bin/"
