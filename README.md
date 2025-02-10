# ClamAV API

A RESTful API service for ClamAV antivirus scanning, built with Go. This service provides a simple HTTP interface to ClamAV's antivirus scanning capabilities, making it easy to integrate virus scanning into your applications.

## Features

- 🔍 Real-time virus scanning via REST API
- 🚀 High-performance Go implementation
- 🔄 Automatic ClamAV database updates
- 🏗️ Multi-architecture support (amd64, arm64, arm/v7, arm/v6)
- 🐳 Docker and docker-compose support
- ⚙️ Configurable via environment variables or CLI flags
- 🔬 Comprehensive test coverage
- 🏥 Health check endpoint for monitoring
- 📊 Scan timing metrics in responses

## Quick Start

### Using Docker Compose

```bash
docker compose up
```

### API Usage

Scan a file:
```bash
curl -F "file=@/path/to/file" http://localhost:6000/api/scan
```

Check service health:
```bash
curl http://localhost:6000/api/health-check
```

## Configuration

Environment variables:
- `GIN_MODE`: Gin framework mode (debug/release/test)
- `CLAMAV_DEBUG`: Enable debug mode (true/false)
- `CLAMAV_SOCKET`: ClamAV Unix socket path
- `CLAMAV_MAX_SIZE`: Maximum file size in bytes
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
