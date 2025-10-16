# Testing Guide

## Overview

The ClamAV API includes comprehensive test coverage for both REST and gRPC implementations, including unit tests, integration tests, and benchmarks.

## Test Structure

```
src/
├── main_test.go            # REST API tests
├── grpc_server_test.go     # gRPC unit tests
└── integration_test.go     # Integration tests
```

## Running Tests

### Quick Start

```bash
# Run all unit tests
make test

# Run all tests including integration
make test-all

# Run only integration tests
make test-integration

# Generate coverage report
make test-coverage

# Run benchmarks
make bench
```

## Unit Tests

### REST API Tests (`main_test.go`)

Tests for the REST API endpoints:

- ✅ Health check endpoint
- ✅ File scanning (multipart upload)
- ✅ Stream scanning (binary upload)
- ✅ EICAR test virus detection
- ✅ Clean file scanning

**Run:**
```bash
cd src
go test -v -run TestScan ./...
go test -v -run TestHealthCheck ./...
```

### gRPC Tests (`grpc_server_test.go`)

Comprehensive tests for gRPC functionality:

#### Health Check
```bash
go test -v -run TestGRPCHealthCheck ./src/
```

Tests:
- Service availability
- Status reporting
- Error handling

#### Unary RPC Tests
```bash
go test -v -run TestGRPCScanFile ./src/
```

Tests:
- Clean file scanning
- EICAR virus detection
- Empty file validation
- File size limits
- Error responses

#### Streaming Tests
```bash
go test -v -run TestGRPCScanStream ./src/
```

Tests:
- Client streaming
- Chunked data transfer
- Large file handling
- Stream completion
- Size limit enforcement

#### Bidirectional Streaming
```bash
go test -v -run TestGRPCScanMultiple ./src/
```

Tests:
- Multiple file scanning
- Concurrent response handling
- Mixed clean/infected files
- Stream lifecycle management

#### Special Cases
```bash
go test -v -run TestGRPCContext ./src/
```

Tests:
- Context cancellation
- Timeout handling
- Request cancellation

## Integration Tests

Integration tests verify the complete system behavior with both REST and gRPC APIs.

### Prerequisites

1. Start the services:
```bash
docker compose up
```

2. Wait for services to be ready:
```bash
# Check REST API
curl http://localhost:6000/api/health-check

# Check gRPC API
grpcurl -plaintext localhost:9000 clamav.ClamAVScanner/HealthCheck
```

### Running Integration Tests

```bash
# Run all integration tests
go test -v -tags=integration ./src/

# Run specific integration test
go test -v -tags=integration -run TestIntegrationPerformance ./src/
```

### Integration Test Coverage

#### Performance Comparison
```bash
go test -v -tags=integration -run TestIntegrationRESTvsGRPCPerformance ./src/
```

Compares:
- Request/response times
- Throughput
- Resource usage

Example output:
```
REST API duration: 15.2ms
gRPC API duration: 9.8ms
Performance improvement: 35.53%
```

#### Concurrent Scanning
```bash
go test -v -tags=integration -run TestIntegrationConcurrentScans ./src/
```

Tests:
- 10 concurrent REST requests
- 10 concurrent gRPC requests
- Error rate under load
- Resource contention

#### Large File Streaming
```bash
go test -v -tags=integration -run TestIntegrationStreamingLargeFile ./src/
```

Tests:
- 10MB file streaming
- Chunked transfer
- Memory efficiency
- Scan completion

#### Bidirectional Streaming
```bash
go test -v -tags=integration -run TestIntegrationBidirectionalStreaming ./src/
```

Tests:
- Multiple file batch scanning
- Mixed clean/infected files
- Response ordering
- Stream management

## Benchmarks

### Running Benchmarks

```bash
# All benchmarks
make bench

# Specific benchmark
cd src && go test -bench=BenchmarkGRPCScanFile -benchmem

# With CPU profiling
cd src && go test -bench=. -cpuprofile=cpu.prof
go tool pprof cpu.prof
```

### Available Benchmarks

#### gRPC Unary Scan
```bash
go test -bench=BenchmarkGRPCScanFile -benchmem ./src/
```

Measures:
- Requests per second
- Memory allocations
- Allocation size

Example output:
```
BenchmarkGRPCScanFile-8    500    2341234 ns/op    4156 B/op    42 allocs/op
```

#### gRPC Streaming Scan
```bash
go test -bench=BenchmarkGRPCScanStream -benchmem ./src/
```

Measures:
- Streaming throughput
- Memory efficiency
- Chunk processing speed

## Coverage Reports

### Generate Coverage

```bash
make test-coverage
```

This generates:
- `src/coverage.txt` - Machine-readable coverage data
- `src/coverage.html` - HTML coverage report

### View Coverage

```bash
# In browser
open src/coverage.html

# In terminal
cd src && go tool cover -func=coverage.out
```

### Coverage Goals

Target coverage levels:
- Overall: > 80%
- Core handlers: > 90%
- gRPC methods: > 85%
- Error paths: > 75%

## Test Utilities

### Using bufconn for Testing

The gRPC tests use `bufconn` for in-memory testing without network overhead:

```go
lis := bufconn.Listen(bufSize)
s := grpc.NewServer()
pb.RegisterClamAVScannerServer(s, NewGRPCServer())

// Connect to in-memory server
conn, _ := grpc.DialContext(ctx, "bufnet",
    grpc.WithContextDialer(bufDialer),
    grpc.WithTransportCredentials(insecure.NewCredentials()))
```

Benefits:
- Fast test execution
- No port conflicts
- Isolated testing
- Deterministic behavior

### Test Data

#### EICAR Test Virus

Used for virus detection testing:
```go
eicarData := []byte(`X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`)
```

Safe test file that:
- All AV engines detect
- No actual malware
- Standard testing signature

## CI/CD Integration

### GitHub Actions

Tests run automatically on:
- Pull requests
- Push to main branch
- Tag creation

Workflow steps:
1. Install ClamAV
2. Update virus definitions
3. Run unit tests
4. Run gRPC tests
5. Upload coverage reports

### Local CI Simulation

```bash
# Simulate CI environment
docker run --rm -v $(pwd):/app -w /app golang:1.20 bash -c "
  apt-get update && 
  apt-get install -y clamav clamav-daemon &&
  freshclam &&
  cd src &&
  go test -v -short ./...
"
```

## Troubleshooting

### ClamAV Not Running

If tests fail with "ClamAV service unavailable":

```bash
# Check ClamAV status
sudo systemctl status clamav-daemon

# Start ClamAV
sudo systemctl start clamav-daemon

# Check socket
ls -l /var/run/clamav/clamd.ctl
```

### Port Conflicts

If integration tests fail with "address already in use":

```bash
# Check what's using the ports
lsof -i :6000
lsof -i :9000

# Stop conflicting services
docker compose down
```

### Test Timeouts

If tests timeout:

1. Increase timeout in config:
```go
config.ScanTimeout = 600 * time.Second
```

2. Or skip slow tests:
```bash
go test -short ./...
```

### gRPC Connection Issues

If gRPC tests fail to connect:

```bash
# Check gRPC server is running
grpcurl -plaintext localhost:9000 list

# Enable gRPC logging
export GRPC_GO_LOG_VERBOSITY_LEVEL=99
export GRPC_GO_LOG_SEVERITY_LEVEL=info
```

## Best Practices

### Writing Tests

1. **Use table-driven tests**:
```go
tests := []struct {
    name string
    data []byte
    want string
}{
    {"clean file", []byte("clean"), "OK"},
    {"eicar", eicarData, "FOUND"},
}
```

2. **Test error paths**:
```go
_, err := client.ScanFile(ctx, &pb.ScanFileRequest{})
assert.Error(t, err)
assert.Contains(t, err.Error(), "expected error")
```

3. **Use subtests**:
```go
t.Run("CleanFile", func(t *testing.T) {
    // test clean file
})
t.Run("InfectedFile", func(t *testing.T) {
    // test infected file
})
```

4. **Clean up resources**:
```go
t.Cleanup(func() {
    conn.Close()
    stream.CloseSend()
})
```

### Performance Testing

1. **Use benchmarks for comparison**:
```bash
# Before changes
go test -bench=. > old.txt

# After changes
go test -bench=. > new.txt

# Compare
benchstat old.txt new.txt
```

2. **Profile memory**:
```bash
go test -bench=. -memprofile=mem.prof
go tool pprof mem.prof
```

3. **Profile CPU**:
```bash
go test -bench=. -cpuprofile=cpu.prof
go tool pprof cpu.prof
```

## Test Maintenance

### Regular Tasks

1. **Update EICAR signature** if ClamAV changes detection
2. **Review timeout values** for CI environments
3. **Update benchmark baselines** after optimizations
4. **Verify coverage metrics** remain above targets
5. **Test with latest ClamAV versions**

### Adding New Tests

When adding features:
1. Add unit tests for new functions
2. Add integration tests for new endpoints
3. Update benchmark suite if performance-critical
4. Update this documentation
5. Verify CI passes

## Resources

- [Go Testing Documentation](https://golang.org/pkg/testing/)
- [testify Package](https://github.com/stretchr/testify)
- [gRPC Testing Guide](https://grpc.io/docs/languages/go/basics/#testing)
- [ClamAV Test Files](https://www.eicar.org/download-anti-malware-testfile/)

