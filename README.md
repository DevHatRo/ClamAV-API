# ClamAV API

A RESTful API service for ClamAV antivirus scanning, built with Go. This service provides a simple HTTP interface to ClamAV's antivirus scanning capabilities, making it easy to integrate virus scanning into your applications.

## Features

- 🔍 Real-time virus scanning via REST API and gRPC
- 🚀 High-performance Go implementation
- 🌊 Streaming scan support for large files
- ⚡ gRPC support with bidirectional streaming
- 📝 Structured logging with Uber Zap
- 🔄 Automatic ClamAV database updates
- 🏗️ Multi-architecture support (amd64, arm64, arm/v7, arm/v6)
- 🐳 Docker and docker-compose support
- ⚙️ Configurable via environment variables or CLI flags
- 🔬 Comprehensive test coverage
- 🏥 Health check endpoint for monitoring
- 📊 Scan timing metrics in responses
- 🎯 Helm chart for Kubernetes deployment

## Quick Start

### Using Helm

```bash
# Add the DevHat Helm repository
helm repo add devhat https://devhatro.github.io/helm-charts

# Update Helm repositories
helm repo update

# Install ClamAV API
helm install clamav-api devhat/clamav-api
```

#### Helm Configuration

You can customize the installation by creating a values.yaml file:

```yaml
# values.yaml
replicaCount: 2

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: clamav.example.com
      paths:
        - path: /
          pathType: Prefix

persistence:
  enabled: true
  size: 5Gi

resources:
  limits:
    cpu: 1000m
    memory: 2Gi
  requests:
    cpu: 100m
    memory: 1Gi

config:
  debug: false
  maxSize: "209715200"
```

Then install with custom values:
```bash
helm install clamav-api devhat/clamav-api -f values.yaml
```

### Using Docker Compose

```bash
docker compose up
```

### REST API Usage

#### Health Check
```bash
curl http://localhost:6000/api/health-check
```

#### Version Info
```bash
curl http://localhost:6000/api/version
```

#### Scan File (Multipart Upload)
```bash
curl -F "file=@/path/to/file" http://localhost:6000/api/scan
```

#### Stream Scan (Direct Binary Upload)
```bash
# Stream scan - useful for large files or when you want to stream data directly
curl -X POST \
  --data-binary "@/path/to/file" \
  -H "Content-Type: application/octet-stream" \
  http://localhost:6000/api/stream-scan

# Or pipe data directly
cat /path/to/file | curl -X POST \
  --data-binary @- \
  -H "Content-Type: application/octet-stream" \
  http://localhost:6000/api/stream-scan
```

### gRPC API Usage

The service exposes a gRPC API on port 9000 (configurable) with the following methods:

- `HealthCheck`: Check service health
- `ScanFile`: Scan a file with unary RPC
- `ScanStream`: Scan with client streaming (for large files)
- `ScanMultiple`: Scan multiple files with bidirectional streaming

#### Using grpcurl

```bash
# Install grpcurl
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest

# Health check
grpcurl -plaintext localhost:9000 clamav.ClamAVScanner/HealthCheck

# List available services
grpcurl -plaintext localhost:9000 list

# Describe service
grpcurl -plaintext localhost:9000 describe clamav.ClamAVScanner
```

#### Example Go Client

```go
package main

import (
    "context"
    "log"
    
    pb "clamav-api/proto"
    "google.golang.org/grpc"
)

func main() {
    conn, err := grpc.Dial("localhost:9000", grpc.WithInsecure())
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()
    
    client := pb.NewClamAVScannerClient(conn)
    
    // Health check
    resp, err := client.HealthCheck(context.Background(), &pb.HealthCheckRequest{})
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Health: %s", resp.Status)
}
```

## Configuration

Environment variables:
- `GIN_MODE`: Gin framework mode (debug/release/test)
- `CLAMAV_DEBUG`: Enable debug mode (true/false)
- `CLAMAV_SOCKET`: ClamAV Unix socket path
- `CLAMAV_MAX_SIZE`: Maximum file size in bytes
- `CLAMAV_SCAN_TIMEOUT`: Scan timeout in seconds (default: 300)
- `CLAMAV_HOST`: Host to listen on
- `CLAMAV_PORT`: REST API port (default: 6000)
- `CLAMAV_GRPC_PORT`: gRPC server port (default: 9000)
- `CLAMAV_ENABLE_GRPC`: Enable gRPC server (default: true)

Command line flags:

```bash
./clamav-api -h
  -debug
        Enable debug mode
  -enable-grpc
        Enable gRPC server (default true)
  -grpc-port string
        gRPC server port (default "9000")
  -host string
        Host to listen on (default "0.0.0.0")
  -max-size int
        Maximum file size in bytes (default 209715200)
  -port string
        Port to listen on (default "6000")
  -scan-timeout int
        Scan timeout in seconds (default 300)
  -socket string
        ClamAV Unix socket path (default "/run/clamav/clamd.ctl")
```

## API Response Examples

### Health Check Response
```json
{
    "message": "ok"
}
```

### Version Response
```json
{
    "version": "1.3.0",
    "commit": "abc1234",
    "build": "2025-10-16T12:00:00Z"
}
```

### Scan Response (Clean File)
```json
{
    "status": "OK",
    "message": "",
    "time": 0.001234
}
```

### Scan Response (Infected File)
```json
{
    "status": "FOUND",
    "message": "Eicar-Test-Signature",
    "time": 0.002342
}
```

### Scan Response (Timeout — HTTP 504)
```json
{
    "status": "Scan timeout",
    "message": "scan operation timed out after 300 seconds"
}
```

### Scan Response (Client Canceled — HTTP 499)
```json
{
    "status": "Client closed request",
    "message": "request canceled by client"
}
```

### Scan Response (ClamAV Unavailable — HTTP 502)
```json
{
    "status": "Clamd service down",
    "message": "Scanning service unavailable"
}
```

## gRPC vs REST API

### REST API
- Simple HTTP interface
- Easy to test with curl
- Works with any HTTP client
- Good for simple integrations

### gRPC API
- ✅ True parallel streaming
- ✅ Efficient binary protocol (Protocol Buffers)
- ✅ Built-in bidirectional streaming
- ✅ Better performance for microservices
- ✅ Strong typing with proto definitions
- ✅ Native support for multiple languages
- ✅ HTTP/2 multiplexing

## Logging

The application uses structured logging with [Uber Zap](https://github.com/uber-go/zap) for high-performance, structured logging.

### Log Levels

- **Production mode**: INFO level and above
- **Development mode**: DEBUG level and above

### Logged Events

**REST API:**
- File uploads with size and client IP
- Scan results (status, virus name, timing)
- Errors and timeouts
- Health check results

**gRPC API:**
- RPC method calls with parameters
- Scan results with timing
- Stream progress and errors
- Context cancellations

**System:**
- Server startup and configuration
- Connection errors
- Graceful shutdown signals

### Log Format

```json
{
  "level": "info",
  "timestamp": "2025-10-16T12:00:00Z",
  "msg": "Scan completed",
  "filename": "test.pdf",
  "status": "OK",
  "elapsed_seconds": 0.123,
  "client_ip": "192.168.1.1"
}
```

### Configuration

Set log level via environment:
```bash
ENV=production  # INFO level
ENV=development # DEBUG level
CLAMAV_DEBUG=true # DEBUG level regardless of ENV
```

## Observability

### Prometheus Metrics

The service exposes a `/metrics` endpoint with Prometheus-compatible metrics:

- `clamav_scan_requests_total` — Total scan requests by method and result status
- `clamav_scan_duration_seconds` — Scan duration histogram
- `clamav_scans_in_progress` — Number of scans currently in progress
- `clamav_http_requests_total` — Total HTTP requests by method, path, and status code
- `clamav_http_request_duration_seconds` — HTTP request duration histogram
- `clamav_health_check_healthy` — Whether ClamAV is healthy (1) or unhealthy (0)

```bash
curl http://localhost:6000/metrics
```

## Security Features

- ✅ Content-Length validation (stream scan requires valid Content-Length header)
- ✅ Size enforcement with `io.LimitedReader` to prevent memory exhaustion
- ✅ Scan timeout protection (configurable, default 300 seconds)
- ✅ Channel cleanup to prevent goroutine leaks
- ✅ DoS protection through size limits and timeouts
- ✅ Structured audit logging for security monitoring

## Development

### Prerequisites
- Go 1.26 or later
- ClamAV
- Docker (optional)

### Testing

The test suite requires a running ClamAV daemon. Set `CLAMAV_SOCKET` to point at the clamd socket.

#### Run All Tests
```bash
# Run all tests with ClamAV (recommended)
CLAMAV_SOCKET=/var/run/clamav/clamd.ctl make test-all

# Run unit tests only (skips some ClamAV-dependent assertions)
make test
```

#### Integration Tests
```bash
# Run integration tests (requires running service via docker compose)
make test-integration
```

#### Coverage Report
```bash
# Generate coverage report
make test-coverage

# View coverage in browser
open src/coverage.html
```

#### Benchmarks
```bash
# Run performance benchmarks
make bench
```

#### Test Files

| File | Coverage Area |
|------|--------------|
| `config_test.go` | Configuration parsing, env var overrides, validation exits, Gin modes |
| `handlers_test.go` | REST endpoints, error responses (502/504/499), version endpoint |
| `grpc_server_test.go` | gRPC health check, scan methods, error code mapping, invalid socket handling |
| `scanner_test.go` | ClamAV scan execution, timeout, context cancellation, error types |
| `streaming_test.go` | Large file scanning, chunk sizes, special filenames, content types |
| `metrics_test.go` | Prometheus metrics middleware, scan metrics recording |
| `logger_test.go` | Logger initialization (production/development), sync |
| `shutdown_test.go` | Graceful shutdown for REST and gRPC servers |
| `main_test.go` | End-to-end REST API scan and stream-scan tests |
| `integration_test.go` | Cross-API performance, concurrent scanning, bidirectional streaming |

Run integration tests with:
```bash
# Start the service first
docker compose up

# In another terminal
go test -v -tags=integration ./src/
```

### Building Multi-arch Images
```bash
docker buildx build --platform linux/amd64,linux/arm64 -t clamav-api:test .
```

### Building Locally

**Prerequisites:**
- Protocol Buffers compiler (`protoc`)
- Go 1.26 or later

**Install protoc:**
```bash
# macOS
brew install protobuf

# Ubuntu/Debian  
apt-get install protobuf-compiler

# Or download from https://github.com/protocolbuffers/protobuf/releases
```

**Generate proto files and build:**
```bash
# Generate proto files
./scripts/generate-proto.sh

# Build
cd src
go build -o ../clamav-api .
```

**Note:** If building without generating proto files, the Docker build will handle proto generation automatically.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
