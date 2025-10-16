//go:build integration
// +build integration

package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	pb "clamav-api/proto"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	restAPIURL  = "http://localhost:6000"
	grpcAPIAddr = "localhost:9000"
)

// TestIntegrationRESTvsGRPCPerformance compares REST and gRPC performance
func TestIntegrationRESTvsGRPCPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testData := bytes.Repeat([]byte("Performance test data"), 1000) // ~20KB

	// Test REST API
	restStart := time.Now()
	restResp, err := http.Post(restAPIURL+"/api/stream-scan",
		"application/octet-stream",
		bytes.NewReader(testData))
	assert.NoError(t, err)
	defer restResp.Body.Close()
	restDuration := time.Since(restStart)

	// Test gRPC API
	conn, err := grpc.Dial(grpcAPIAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.NoError(t, err)
	defer conn.Close()

	client := pb.NewClamAVScannerClient(conn)

	grpcStart := time.Now()
	_, err = client.ScanFile(context.Background(), &pb.ScanFileRequest{
		Data:     testData,
		Filename: "perf-test.bin",
	})
	assert.NoError(t, err)
	grpcDuration := time.Since(grpcStart)

	t.Logf("REST API duration: %v", restDuration)
	t.Logf("gRPC API duration: %v", grpcDuration)
	t.Logf("Performance improvement: %.2f%%",
		float64(restDuration-grpcDuration)/float64(restDuration)*100)

	// gRPC should generally be faster
	assert.Less(t, grpcDuration, restDuration*2, "gRPC should not be significantly slower than REST")
}

// TestIntegrationConcurrentScans tests concurrent scanning on both APIs
func TestIntegrationConcurrentScans(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	const numRequests = 10

	// Setup gRPC client
	conn, err := grpc.Dial(grpcAPIAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.NoError(t, err)
	defer conn.Close()

	client := pb.NewClamAVScannerClient(conn)

	// Test concurrent gRPC requests
	t.Run("ConcurrentgRPC", func(t *testing.T) {
		done := make(chan bool, numRequests)
		errors := make(chan error, numRequests)

		for i := 0; i < numRequests; i++ {
			go func(id int) {
				data := []byte("Concurrent test file")
				_, err := client.ScanFile(context.Background(), &pb.ScanFileRequest{
					Data:     data,
					Filename: "concurrent.txt",
				})
				if err != nil {
					errors <- err
				}
				done <- true
			}(i)
		}

		// Wait for all requests
		for i := 0; i < numRequests; i++ {
			<-done
		}
		close(errors)

		// Check for errors
		errorCount := 0
		for err := range errors {
			t.Logf("Error: %v", err)
			errorCount++
		}

		assert.Equal(t, 0, errorCount, "No errors should occur during concurrent scans")
	})

	// Test concurrent REST requests
	t.Run("ConcurrentREST", func(t *testing.T) {
		done := make(chan bool, numRequests)
		errors := make(chan error, numRequests)

		for i := 0; i < numRequests; i++ {
			go func(id int) {
				data := []byte("Concurrent test file")
				resp, err := http.Post(restAPIURL+"/api/stream-scan",
					"application/octet-stream",
					bytes.NewReader(data))
				if err != nil {
					errors <- err
				} else {
					resp.Body.Close()
				}
				done <- true
			}(i)
		}

		// Wait for all requests
		for i := 0; i < numRequests; i++ {
			<-done
		}
		close(errors)

		// Check for errors
		errorCount := 0
		for err := range errors {
			t.Logf("Error: %v", err)
			errorCount++
		}

		assert.Equal(t, 0, errorCount, "No errors should occur during concurrent scans")
	})
}

// TestIntegrationStreamingLargeFile tests streaming a large file
func TestIntegrationStreamingLargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a 10MB test file
	largeData := make([]byte, 10*1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	conn, err := grpc.Dial(grpcAPIAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.NoError(t, err)
	defer conn.Close()

	client := pb.NewClamAVScannerClient(conn)

	stream, err := client.ScanStream(context.Background())
	assert.NoError(t, err)

	// Send in 1MB chunks
	chunkSize := 1024 * 1024
	for i := 0; i < len(largeData); i += chunkSize {
		end := i + chunkSize
		if end > len(largeData) {
			end = len(largeData)
		}

		isLast := end == len(largeData)
		filename := ""
		if i == 0 {
			filename = "large-file.bin"
		}

		err := stream.Send(&pb.ScanStreamRequest{
			Chunk:    largeData[i:end],
			Filename: filename,
			IsLast:   isLast,
		})
		assert.NoError(t, err)
	}

	resp, err := stream.CloseAndRecv()
	assert.NoError(t, err)
	assert.Equal(t, "OK", resp.Status)
	t.Logf("Large file scan completed in %.3fs", resp.ScanTime)
}

// TestIntegrationBidirectionalStreaming tests multiple file scanning
func TestIntegrationBidirectionalStreaming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	conn, err := grpc.Dial(grpcAPIAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.NoError(t, err)
	defer conn.Close()

	client := pb.NewClamAVScannerClient(conn)

	stream, err := client.ScanMultiple(context.Background())
	assert.NoError(t, err)

	// Prepare multiple files
	files := []struct {
		name string
		data []byte
	}{
		{"clean1.txt", []byte("Clean file number 1")},
		{"clean2.txt", []byte("Clean file number 2")},
		{"clean3.txt", []byte("Clean file number 3")},
		{"eicar.txt", []byte(`X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`)},
		{"clean4.txt", []byte("Clean file number 4")},
	}

	// Send and receive concurrently
	errChan := make(chan error, 1)
	respChan := make(chan *pb.ScanResponse, len(files))

	// Goroutine to receive responses
	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				close(respChan)
				return
			}
			if err != nil {
				errChan <- err
				return
			}
			respChan <- resp
		}
	}()

	// Send all files
	for _, file := range files {
		err := stream.Send(&pb.ScanStreamRequest{
			Chunk:    file.data,
			Filename: file.name,
			IsLast:   true,
		})
		assert.NoError(t, err)
	}

	err = stream.CloseSend()
	assert.NoError(t, err)

	// Collect all responses
	responses := make([]*pb.ScanResponse, 0)
	for resp := range respChan {
		responses = append(responses, resp)
		t.Logf("Scanned: %s - %s - %s (%.3fs)",
			resp.Filename, resp.Status, resp.Message, resp.ScanTime)
	}

	// Verify
	select {
	case err := <-errChan:
		t.Fatal(err)
	default:
	}

	assert.Equal(t, len(files), len(responses))

	// Verify EICAR was detected
	foundEicar := false
	for _, resp := range responses {
		if resp.Filename == "eicar.txt" {
			assert.Equal(t, "FOUND", resp.Status)
			foundEicar = true
		} else {
			assert.Equal(t, "OK", resp.Status)
		}
	}
	assert.True(t, foundEicar, "EICAR file should be detected")
}

// TestIntegrationHealthCheck tests health check on both APIs
func TestIntegrationHealthCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test REST health check
	t.Run("REST", func(t *testing.T) {
		resp, err := http.Get(restAPIURL + "/api/health-check")
		assert.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// Test gRPC health check
	t.Run("gRPC", func(t *testing.T) {
		conn, err := grpc.Dial(grpcAPIAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		assert.NoError(t, err)
		defer conn.Close()

		client := pb.NewClamAVScannerClient(conn)
		resp, err := client.HealthCheck(context.Background(), &pb.HealthCheckRequest{})
		assert.NoError(t, err)
		assert.Equal(t, "healthy", resp.Status)
	})
}

// TestIntegrationTimeout tests scan timeout on both APIs
func TestIntegrationTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	conn, err := grpc.Dial(grpcAPIAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.NoError(t, err)
	defer conn.Close()

	client := pb.NewClamAVScannerClient(conn)

	// This should timeout
	_, err = client.ScanFile(ctx, &pb.ScanFileRequest{
		Data:     []byte("test data"),
		Filename: "timeout-test.txt",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}
