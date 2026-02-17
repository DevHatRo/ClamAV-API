package main

import (
	"context"
	"fmt"
	"io"
	"time"
)

// ScanResult holds the outcome of a ClamAV scan
type ScanResult struct {
	Status      string
	Description string
	ScanTime    float64
}

// ScanTimeoutError indicates the scan exceeded the configured timeout
type ScanTimeoutError struct {
	Timeout time.Duration
}

func (e *ScanTimeoutError) Error() string {
	return fmt.Sprintf("scan operation timed out after %.0f seconds", e.Timeout.Seconds())
}

// ScanEngineError indicates ClamAV returned an error during scanning
type ScanEngineError struct {
	Description string
	ScanTime    float64
}

func (e *ScanEngineError) Error() string {
	return e.Description
}

// performScan executes a ClamAV scan on the given reader.
// It respects both the configured timeout and context cancellation.
func performScan(ctx context.Context, reader io.Reader, timeout time.Duration) (*ScanResult, error) {
	clam := getClamdClient()

	startTime := time.Now()

	done := make(chan bool)
	defer close(done)

	response, err := clam.ScanStream(reader, done)
	if err != nil {
		return nil, fmt.Errorf("clamd unavailable: %w", err)
	}

	select {
	case result := <-response:
		elapsed := time.Since(startTime).Seconds()

		if result.Status == "ERROR" {
			return nil, &ScanEngineError{
				Description: result.Description,
				ScanTime:    elapsed,
			}
		}

		return &ScanResult{
			Status:      result.Status,
			Description: result.Description,
			ScanTime:    elapsed,
		}, nil

	case <-time.After(timeout):
		go func() { for range response {} }()
		return nil, &ScanTimeoutError{Timeout: timeout}

	case <-ctx.Done():
		go func() { for range response {} }()
		return nil, ctx.Err()
	}
}
