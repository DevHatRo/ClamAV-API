# gRPC Implementation Guide

## Overview

The ClamAV API supports both REST and gRPC interfaces. The gRPC implementation provides high-performance binary protocol communication with support for streaming and bidirectional communication.

## Protocol Buffer Definition

The service is defined in `proto/clamav.proto`:

```protobuf
service ClamAVScanner {
  rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
  rpc ScanFile(ScanFileRequest) returns (ScanResponse);
  rpc ScanStream(stream ScanStreamRequest) returns (ScanResponse);
  rpc ScanMultiple(stream ScanStreamRequest) returns (stream ScanResponse);
}
```

## Setup

### Prerequisites

1. Install Protocol Buffers compiler:
```bash
# macOS
brew install protobuf

# Ubuntu/Debian
apt-get install protobuf-compiler

# Or download from https://github.com/protocolbuffers/protobuf/releases
```

2. Install Go plugins:
```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

### Generate Code

```bash
# Use the provided script
./scripts/generate-proto.sh

# Or use Make
make proto
```

## RPC Methods

### 1. HealthCheck (Unary)

Check if the ClamAV service is healthy.

**Request:**
```protobuf
message HealthCheckRequest {}
```

**Response:**
```protobuf
message HealthCheckResponse {
  string status = 1;   // "healthy" or "unhealthy"
  string message = 2;  // Additional details
}
```

**Example:**
```bash
grpcurl -plaintext localhost:9000 clamav.ClamAVScanner/HealthCheck
```

### 2. ScanFile (Unary)

Scan a file with a single request/response.

**Request:**
```protobuf
message ScanFileRequest {
  bytes data = 1;       // File content
  string filename = 2;  // Optional filename
}
```

**Response:**
```protobuf
message ScanResponse {
  string status = 1;     // "OK", "FOUND", or "ERROR"
  string message = 2;    // Virus name or error message
  double scan_time = 3;  // Scan duration in seconds
  string filename = 4;   // Filename if provided
}
```

### 3. ScanStream (Client Streaming)

Stream file chunks to the server for scanning large files.

**Request:**
```protobuf
message ScanStreamRequest {
  bytes chunk = 1;       // File chunk
  string filename = 2;   // Filename (sent with first chunk)
  bool is_last = 3;      // True for the last chunk
}
```

**Response:**
```protobuf
message ScanResponse {
  // Same as ScanFile
}
```

### 4. ScanMultiple (Bidirectional Streaming)

Scan multiple files with bidirectional streaming.

**Request:** Stream of `ScanStreamRequest` messages

**Response:** Stream of `ScanResponse` messages

## Error Handling

The gRPC API uses standard gRPC status codes to report errors:

| Scenario | gRPC Code | Description |
|----------|-----------|-------------|
| Empty file data | `INVALID_ARGUMENT` | `file data is required` |
| File exceeds size limit | `INVALID_ARGUMENT` | `file too large, maximum size is N bytes` |
| ClamAV daemon unavailable | `INTERNAL` | `scan failed: clamd unavailable: ...` |
| Scan engine error | `INTERNAL` | `scan error: <description>` |
| Scan timeout | `DEADLINE_EXCEEDED` | `scan operation timed out after N seconds` |
| Client cancellation | `CANCELED` | `request canceled by client` |

For `ScanMultiple` (bidirectional streaming), per-file errors are returned in the response message with `status: "ERROR"` rather than terminating the stream, allowing the remaining files to be scanned.

## Client Examples

### Go Client

```go
package main

import (
    "context"
    "io"
    "log"
    "os"
    
    pb "clamav-api/proto"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

func main() {
    // Connect to server
    conn, err := grpc.Dial("localhost:9000", 
        grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()
    
    client := pb.NewClamAVScannerClient(conn)
    
    // Example 1: Health Check
    healthResp, err := client.HealthCheck(context.Background(), 
        &pb.HealthCheckRequest{})
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Health: %s - %s", healthResp.Status, healthResp.Message)
    
    // Example 2: Scan File (Unary)
    data, _ := os.ReadFile("test.txt")
    scanResp, err := client.ScanFile(context.Background(), 
        &pb.ScanFileRequest{
            Data:     data,
            Filename: "test.txt",
        })
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Scan result: %s - %s (%.3fs)", 
        scanResp.Status, scanResp.Message, scanResp.ScanTime)
    
    // Example 3: Stream Scan
    stream, err := client.ScanStream(context.Background())
    if err != nil {
        log.Fatal(err)
    }
    
    // Send chunks
    chunkSize := 64 * 1024 // 64KB chunks
    for i := 0; i < len(data); i += chunkSize {
        end := i + chunkSize
        if end > len(data) {
            end = len(data)
        }
        
        isLast := end == len(data)
        filename := ""
        if i == 0 {
            filename = "large-file.bin"
        }
        
        if err := stream.Send(&pb.ScanStreamRequest{
            Chunk:    data[i:end],
            Filename: filename,
            IsLast:   isLast,
        }); err != nil {
            log.Fatal(err)
        }
    }
    
    // Receive response
    resp, err := stream.CloseAndRecv()
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Stream scan result: %s - %s", resp.Status, resp.Message)
}
```

### Python Client

```python
import grpc
import clamav_pb2
import clamav_pb2_grpc

# Connect to server
channel = grpc.insecure_channel('localhost:9000')
stub = clamav_pb2_grpc.ClamAVScannerStub(channel)

# Health check
response = stub.HealthCheck(clamav_pb2.HealthCheckRequest())
print(f"Health: {response.status} - {response.message}")

# Scan file
with open('test.txt', 'rb') as f:
    data = f.read()
    
response = stub.ScanFile(clamav_pb2.ScanFileRequest(
    data=data,
    filename='test.txt'
))
print(f"Scan: {response.status} - {response.message}")
```

### Node.js Client

```javascript
const grpc = require('@grpc/grpc-js');
const protoLoader = require('@grpc/proto-loader');

const packageDefinition = protoLoader.loadSync('proto/clamav.proto');
const proto = grpc.loadPackageDefinition(packageDefinition).clamav;

const client = new proto.ClamAVScanner('localhost:9000',
    grpc.credentials.createInsecure());

// Health check
client.HealthCheck({}, (err, response) => {
    if (err) {
        console.error(err);
        return;
    }
    console.log(`Health: ${response.status} - ${response.message}`);
});

// Scan file
const fs = require('fs');
const data = fs.readFileSync('test.txt');

client.ScanFile({ data, filename: 'test.txt' }, (err, response) => {
    if (err) {
        console.error(err);
        return;
    }
    console.log(`Scan: ${response.status} - ${response.message}`);
});
```

## Performance Benefits

### REST vs gRPC Comparison

| Feature | REST | gRPC |
|---------|------|------|
| Protocol | HTTP/1.1 | HTTP/2 |
| Data Format | JSON | Protocol Buffers |
| Streaming | Limited | Native support |
| Performance | Good | Excellent |
| Browser Support | Native | Requires grpc-web |
| Payload Size | Larger | Smaller (binary) |
| Type Safety | Manual | Generated code |

### Benchmarks

Typical performance improvements with gRPC:
- 30-40% faster request/response times
- 60-70% smaller payload sizes
- Better CPU and memory efficiency
- Native streaming support

## Configuration

### Environment Variables

- `CLAMAV_ENABLE_GRPC`: Enable/disable gRPC server (default: true)
- `CLAMAV_GRPC_PORT`: gRPC server port (default: 9000)
- `CLAMAV_HOST`: Host for both REST and gRPC (default: 0.0.0.0)

### Command Line Flags

```bash
./clamav-api \
  -enable-grpc=true \
  -grpc-port=9000 \
  -host=0.0.0.0
```

## Troubleshooting

### grpcurl Commands

```bash
# List services
grpcurl -plaintext localhost:9000 list

# Describe service
grpcurl -plaintext localhost:9000 describe clamav.ClamAVScanner

# Call method
grpcurl -plaintext -d '{}' localhost:9000 clamav.ClamAVScanner/HealthCheck
```

### Common Issues

1. **"transport: Error while dialing dial tcp: connection refused"**
   - Ensure gRPC server is enabled: `CLAMAV_ENABLE_GRPC=true`
   - Check if port is accessible: `netstat -an | grep 9000`

2. **"unknown service clamav.ClamAVScanner"**
   - Regenerate proto files: `make proto`
   - Rebuild application: `make build`

3. **"Code: Unavailable - ClamAV service unavailable"**
   - Check ClamAV daemon is running
   - Verify socket path is correct

## Security Considerations

### TLS/mTLS

For production deployments, enable TLS:

```go
creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
if err != nil {
    log.Fatal(err)
}

grpcServer := grpc.NewServer(grpc.Creds(creds))
```

### Authentication

Implement authentication with interceptors:

```go
func authInterceptor(ctx context.Context, req interface{}, 
    info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
    // Check authentication
    md, ok := metadata.FromIncomingContext(ctx)
    if !ok {
        return nil, status.Error(codes.Unauthenticated, "missing metadata")
    }
    
    // Validate token
    if !isValidToken(md["authorization"]) {
        return nil, status.Error(codes.Unauthenticated, "invalid token")
    }
    
    return handler(ctx, req)
}

grpcServer := grpc.NewServer(
    grpc.UnaryInterceptor(authInterceptor),
)
```

## Resources

- [gRPC Official Documentation](https://grpc.io/docs/)
- [Protocol Buffers Guide](https://developers.google.com/protocol-buffers)
- [grpcurl Tool](https://github.com/fullstorydev/grpcurl)
- [grpc-web for Browsers](https://github.com/grpc/grpc-web)

