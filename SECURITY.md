# Security Improvements

## Overview

This document outlines the security enhancements implemented in the ClamAV API to protect against common vulnerabilities and attacks.

## Addressed Vulnerabilities

### 1. Content-Length Validation (DoS Protection)

**Problem**: The streaming scan endpoint could be exploited by omitting the Content-Length header or sending chunked transfer encoding, allowing unlimited data upload and potential memory exhaustion.

**Solution**:
- Mandatory Content-Length header validation
- Rejection of requests with missing or invalid Content-Length (≤ 0)
- Double-layer protection with both header check and `io.LimitedReader`

```go
// Reject if Content-Length is missing or invalid
if contentLength <= 0 {
    return 400 Bad Request
}

// Enforce size limit at read level
limitedReader := &io.LimitedReader{
    R: body,
    N: config.MaxContentLength,
}
```

### 2. Scan Timeout Protection

**Problem**: Blocking channel reads without timeout could cause indefinite waiting if scans hang, leading to goroutine accumulation and resource exhaustion.

**Solution**:
- Implemented configurable scan timeout (default: 300 seconds)
- Using `select` statement with `time.After` for timeout handling
- Returns HTTP 504 Gateway Timeout when scan exceeds timeout

```go
select {
case result := <-response:
    // Process result
case <-time.After(config.ScanTimeout):
    return 504 Gateway Timeout
}
```

### 3. Channel Resource Leak Prevention

**Problem**: The `done` channel was created but never closed, potentially causing goroutine leaks depending on the ClamAV library implementation.

**Solution**:
- Added `defer close(done)` immediately after channel creation
- Ensures proper cleanup even in error cases
- Prevents goroutine leaks

```go
done := make(chan bool)
defer close(done) // Cleanup guaranteed
```

## Configuration

### Environment Variables
- `CLAMAV_MAX_SIZE`: Maximum upload size in bytes (default: 209715200 / 200MB)
- `CLAMAV_SCAN_TIMEOUT`: Scan timeout in seconds (default: 300)

### Command Line Flags
```bash
./clamav-api -max-size 209715200 -scan-timeout 300
```

## Security Best Practices

### Production Deployment

1. **Set Appropriate Timeouts**
   ```bash
   export CLAMAV_SCAN_TIMEOUT=180  # 3 minutes for faster response
   ```

2. **Configure Size Limits**
   ```bash
   export CLAMAV_MAX_SIZE=104857600  # 100MB limit
   ```

3. **Enable Rate Limiting**
   - Use reverse proxy (nginx, Traefik) for rate limiting
   - Implement per-IP request limits

4. **Monitor Resources**
   - Set up alerts for high memory usage
   - Monitor scan duration metrics
   - Track timeout occurrences

### Kubernetes Deployment

```yaml
resources:
  limits:
    cpu: 1000m
    memory: 2Gi
  requests:
    cpu: 100m
    memory: 512Mi

env:
  - name: CLAMAV_SCAN_TIMEOUT
    value: "300"
  - name: CLAMAV_MAX_SIZE
    value: "209715200"
```

## Testing Security Features

### Test Content-Length Validation
```bash
# This should fail (no Content-Length)
curl -X POST \
  -H "Transfer-Encoding: chunked" \
  --data-binary "@file" \
  http://localhost:6000/api/stream-scan

# Expected: 400 Bad Request
```

### Test Size Limits
```bash
# Upload file larger than max size
dd if=/dev/zero of=large.bin bs=1M count=300
curl -X POST \
  --data-binary "@large.bin" \
  -H "Content-Type: application/octet-stream" \
  http://localhost:6000/api/stream-scan

# Expected: 400 Bad Request
```

### Test Timeout
```bash
# Set short timeout for testing
export CLAMAV_SCAN_TIMEOUT=5
./clamav-api

# Upload large file to trigger timeout
# Expected: 504 Gateway Timeout after 5 seconds
```

## Mitigation Summary

| Vulnerability | Severity | Status | Mitigation |
|--------------|----------|--------|------------|
| DoS via unlimited upload | High | ✅ Fixed | Content-Length validation + LimitedReader |
| Resource exhaustion (hanging scans) | High | ✅ Fixed | Configurable timeout with select statement |
| Goroutine leaks | Medium | ✅ Fixed | Proper channel cleanup with defer |

## Additional Recommendations

1. **TLS/HTTPS**: Always deploy behind TLS in production
2. **Authentication**: Add API key or OAuth authentication
3. **Logging**: Implement comprehensive audit logging
4. **CORS**: Configure CORS policies appropriately
5. **Input Validation**: Additional validation for file types if needed

## Reporting Security Issues

If you discover a security vulnerability, please report it to:
- Email: security@example.com
- Create a private security advisory on GitHub

Do not disclose security issues publicly until they have been addressed.

## Version History

- **v1.1.0**: Added streaming scan with security improvements
  - Content-Length validation
  - Scan timeout protection
  - Channel cleanup
  - Configurable security parameters

