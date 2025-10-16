# ClamAV API

A RESTful API service for ClamAV antivirus scanning, built with Go. This service provides a simple HTTP interface to ClamAV's antivirus scanning capabilities, making it easy to integrate virus scanning into your applications.

## Features

- 🔍 Real-time virus scanning via REST API
- 🚀 High-performance Go implementation
- 🌊 Streaming scan support for large files
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

### API Usage

#### Health Check
```bash
curl http://localhost:6000/api/health-check
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

## Configuration

Environment variables:
- `GIN_MODE`: Gin framework mode (debug/release/test)
- `CLAMAV_DEBUG`: Enable debug mode (true/false)
- `CLAMAV_SOCKET`: ClamAV Unix socket path
- `CLAMAV_MAX_SIZE`: Maximum file size in bytes
- `CLAMAV_SCAN_TIMEOUT`: Scan timeout in seconds (default: 300)
- `CLAMAV_HOST`: Host to listen on
- `CLAMAV_PORT`: Port to listen on

Command line flags:

```bash
./clamav-api -h
  -debug
        Enable debug mode
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

### Scan Response (Timeout)
```json
{
    "status": "Scan timeout",
    "message": "Scan operation timed out after 300 seconds"
}
```

## Security Features

- ✅ Content-Length validation (stream scan requires valid Content-Length header)
- ✅ Size enforcement with `io.LimitedReader` to prevent memory exhaustion
- ✅ Scan timeout protection (configurable, default 300 seconds)
- ✅ Channel cleanup to prevent goroutine leaks
- ✅ DoS protection through size limits and timeouts

## Development

### Prerequisites
- Go 1.20 or later
- ClamAV
- Docker (optional)

### Testing
```bash
cd src
go test -v ./...
```

### Building Multi-arch Images
```bash
docker buildx build --platform linux/amd64,linux/arm64 -t clamav-api:test .
```

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
