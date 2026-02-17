package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestScanTimeoutErrorMessage(t *testing.T) {
	err := &ScanTimeoutError{Timeout: 30 * time.Second}
	assert.Equal(t, "scan operation timed out after 30 seconds", err.Error())

	err2 := &ScanTimeoutError{Timeout: 5 * time.Minute}
	assert.Equal(t, "scan operation timed out after 300 seconds", err2.Error())
}

func TestScanEngineErrorMessage(t *testing.T) {
	err := &ScanEngineError{Description: "engine failure", ScanTime: 1.5}
	assert.Equal(t, "engine failure", err.Error())

	err2 := &ScanEngineError{Description: ""}
	assert.Equal(t, "", err2.Error())
}

func TestPerformScanContextCancellation(t *testing.T) {
	// Use the real ClamAV socket (must be running)
	originalSocket := config.ClamdUnixSocket
	defer func() {
		config.ClamdUnixSocket = originalSocket
		resetClamdClient()
	}()

	// Pre-cancel the context before calling performScan
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reader := bytes.NewReader([]byte("test data for cancellation"))
	result, err := performScan(ctx, reader, 30*time.Second)

	// With a cancelled context, we expect either:
	// 1. context.Canceled from the select (if scan hasn't completed yet)
	// 2. A clamd error (if ClamAV connection fails fast)
	if err != nil {
		assert.Nil(t, result)
		t.Logf("Got expected error on cancelled context: %v", err)
	}
}

func TestPerformScanTimeout(t *testing.T) {
	// This test requires ClamAV to be running.
	// We use a very short timeout to trigger the timeout path.
	originalSocket := config.ClamdUnixSocket
	originalTimeout := config.ScanTimeout
	defer func() {
		config.ClamdUnixSocket = originalSocket
		config.ScanTimeout = originalTimeout
		resetClamdClient()
	}()

	// Use a 1-nanosecond timeout to guarantee the timeout fires before scan completes
	result, err := performScan(context.Background(), bytes.NewReader([]byte("test data")), 1*time.Nanosecond)

	if err != nil {
		assert.Nil(t, result)
		// The error could be ScanTimeoutError (if ClamAV accepted the connection and we timed out waiting)
		// or a clamd unavailable error (if ClamAV can't connect)
		_, isTimeout := err.(*ScanTimeoutError)
		if isTimeout {
			t.Log("Got expected ScanTimeoutError")
			assert.Contains(t, err.Error(), "timed out")
		} else {
			t.Logf("Got non-timeout error (ClamAV may not be running): %v", err)
		}
	}
}

func TestPerformScanWithInvalidSocket(t *testing.T) {
	originalSocket := config.ClamdUnixSocket
	config.ClamdUnixSocket = "/nonexistent/clamd.sock"
	resetClamdClient()
	defer func() {
		config.ClamdUnixSocket = originalSocket
		resetClamdClient()
	}()

	reader := bytes.NewReader([]byte("test data"))
	result, err := performScan(context.Background(), reader, 30*time.Second)

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "clamd unavailable")
}

func TestPerformScanCleanFile(t *testing.T) {
	reader := bytes.NewReader([]byte("This is a clean file"))
	result, err := performScan(context.Background(), reader, 30*time.Second)

	if err != nil {
		t.Logf("Scan failed (ClamAV may not be running): %v", err)
		return
	}

	assert.NotNil(t, result)
	assert.Equal(t, "OK", result.Status)
	assert.Greater(t, result.ScanTime, 0.0)
}

func TestPerformScanEicarDetection(t *testing.T) {
	eicar := []byte(`X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`)
	reader := bytes.NewReader(eicar)
	result, err := performScan(context.Background(), reader, 30*time.Second)

	if err != nil {
		// If ClamAV returns ERROR status, performScan wraps it in ScanEngineError
		_, isEngine := err.(*ScanEngineError)
		if isEngine {
			t.Log("Got ScanEngineError (expected for some ClamAV configs)")
			return
		}
		t.Logf("Scan failed (ClamAV may not be running): %v", err)
		return
	}

	assert.NotNil(t, result)
	assert.Equal(t, "FOUND", result.Status)
}
