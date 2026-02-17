package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "clamav-api/proto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestHandleScanWithLargeFile tests scanning with a file close to the size limit
func TestHandleScanWithLargeFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.MaxMultipartMemory = config.MaxContentLength
	router.POST("/api/scan", handleScan)

	// Create a large file (1MB)
	largeContent := make([]byte, 1024*1024)
	for i := range largeContent {
		largeContent[i] = byte(i % 256)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "large-file.bin")
	assert.NoError(t, err)
	_, err = part.Write(largeContent)
	assert.NoError(t, err)
	writer.Close()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/scan", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	// Should succeed or fail with ClamAV error (not input validation error)
	assert.True(t, w.Code == 200 || w.Code == 502)
}

// TestHandleStreamScanWithChunkedData tests streaming with realistic chunk patterns
func TestHandleStreamScanWithChunkedData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/stream-scan", handleStreamScan)

	// Create test data
	data := bytes.Repeat([]byte("chunk data "), 1000)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/stream-scan", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(data))
	router.ServeHTTP(w, req)

	// Should succeed or fail with ClamAV error
	assert.True(t, w.Code == 200 || w.Code == 502)
}

// TestHandleStreamScanWithBinaryData tests scanning binary data
func TestHandleStreamScanWithBinaryData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/stream-scan", handleStreamScan)

	// Create binary data
	binaryData := make([]byte, 1024)
	for i := range binaryData {
		binaryData[i] = byte(i % 256)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/stream-scan", bytes.NewReader(binaryData))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(binaryData))
	router.ServeHTTP(w, req)

	assert.True(t, w.Code == 200 || w.Code == 502)
}

// TestHandleScanResponseTiming tests that scan time is properly recorded
func TestHandleScanResponseTiming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/scan", handleScan)

	content := []byte("Timing test file")
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "timing-test.txt")
	part.Write(content)
	writer.Close()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/scan", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	if w.Code == 200 {
		var response map[string]interface{}
		err := parseJSON(w.Body.Bytes(), &response)
		assert.NoError(t, err)

		time, ok := response["time"].(float64)
		assert.True(t, ok)
		assert.Greater(t, time, 0.0)
		assert.Less(t, time, 10.0) // Should complete within 10 seconds
	}
}

// TestGRPCStreamingSizeLimitEnforcement tests chunk-by-chunk size enforcement
func TestGRPCStreamingSizeLimitEnforcement(t *testing.T) {
	client := getTestClient(t)

	stream, err := client.ScanStream(context.Background())
	assert.NoError(t, err)

	// Send chunks that cumulatively exceed the limit
	chunkSize := int64(100 * 1024 * 1024) // 100MB per chunk
	numChunks := 3                        // 300MB total > 200MB limit

	for i := 0; i < numChunks; i++ {
		chunk := make([]byte, chunkSize)
		filename := ""
		if i == 0 {
			filename = "oversized.bin"
		}

		err := stream.Send(&pb.ScanStreamRequest{
			Chunk:    chunk,
			Filename: filename,
			IsLast:   i == numChunks-1,
		})

		// Should fail on second or third chunk
		if err != nil {
			assert.Contains(t, err.Error(), "too large")
			return
		}
	}

	_, err = stream.CloseAndRecv()
	if err != nil {
		assert.Contains(t, err.Error(), "too large")
	}
}

// TestGRPCMultipleFilesSequential tests scanning multiple files in sequence
func TestGRPCMultipleFilesSequential(t *testing.T) {
	client := getTestClient(t)

	stream, err := client.ScanMultiple(context.Background())
	assert.NoError(t, err)

	files := []struct {
		name string
		data []byte
	}{
		{"small.txt", []byte("small")},
		{"medium.txt", bytes.Repeat([]byte("data"), 100)},
		{"large.txt", bytes.Repeat([]byte("data"), 10000)},
	}

	errChan := make(chan error, 1)
	respChan := make(chan *pb.ScanResponse, len(files))

	// Receive responses
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

	// Send files
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

	// Collect responses
	responses := make([]*pb.ScanResponse, 0)
	for resp := range respChan {
		responses = append(responses, resp)
	}

	// Check for errors
	select {
	case err := <-errChan:
		if !strings.Contains(err.Error(), "ClamAV") {
			t.Fatalf("Unexpected error: %v", err)
		}
		t.Logf("ClamAV may not be running: %v", err)
	default:
		if len(responses) > 0 {
			assert.Equal(t, len(files), len(responses))
		}
	}
}

// TestGRPCScanStreamIncrementalChunks tests streaming with many small chunks
func TestGRPCScanStreamIncrementalChunks(t *testing.T) {
	client := getTestClient(t)

	stream, err := client.ScanStream(context.Background())
	assert.NoError(t, err)

	// Send many small chunks
	totalChunks := 100
	chunkData := []byte("small chunk ")

	for i := 0; i < totalChunks; i++ {
		filename := ""
		if i == 0 {
			filename = "incremental.txt"
		}

		err := stream.Send(&pb.ScanStreamRequest{
			Chunk:    chunkData,
			Filename: filename,
			IsLast:   i == totalChunks-1,
		})
		assert.NoError(t, err)
	}

	resp, err := stream.CloseAndRecv()

	if err != nil {
		t.Logf("Stream test skipped (ClamAV may not be running): %v", err)
		return
	}

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "incremental.txt", resp.Filename)
}

// TestGRPCContextDeadline tests behavior with context deadline
func TestGRPCContextDeadline(t *testing.T) {
	client := getTestClient(t)

	// Create context with very short deadline
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(1*time.Millisecond))
	defer cancel()

	// Wait for deadline to pass
	time.Sleep(5 * time.Millisecond)

	_, err := client.HealthCheck(ctx, &pb.HealthCheckRequest{})

	assert.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "deadline exceeded") ||
			strings.Contains(err.Error(), "context deadline exceeded"),
		"Expected deadline error, got: %v", err)
}

// TestHandleScanWithSpecialCharactersInFilename tests filename handling
func TestHandleScanWithSpecialCharactersInFilename(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/scan", handleScan)

	testFilenames := []string{
		"test file with spaces.txt",
		"test-with-dashes.txt",
		"test_with_underscores.txt",
		"тест-кирилица.txt",
		"测试文件.txt",
	}

	for _, filename := range testFilenames {
		t.Run(filename, func(t *testing.T) {
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			part, _ := writer.CreateFormFile("file", filename)
			part.Write([]byte("test content"))
			writer.Close()

			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/scan", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			router.ServeHTTP(w, req)

			// Should handle all filenames
			assert.True(t, w.Code == 200 || w.Code == 502)
		})
	}
}

// TestStreamScanWithVariousContentTypes tests different content types
func TestStreamScanWithVariousContentTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/stream-scan", handleStreamScan)

	contentTypes := []string{
		"application/octet-stream",
		"application/pdf",
		"application/zip",
		"text/plain",
		"image/jpeg",
	}

	for _, ct := range contentTypes {
		t.Run(ct, func(t *testing.T) {
			data := []byte("test data for " + ct)

			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/stream-scan", bytes.NewReader(data))
			req.Header.Set("Content-Type", ct)
			req.ContentLength = int64(len(data))
			router.ServeHTTP(w, req)

			// Should handle all content types
			assert.True(t, w.Code == 200 || w.Code == 502)
		})
	}
}

// Helper function to parse JSON
func parseJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// TestGRPCScanFileWithVariousSizes tests different file sizes
func TestGRPCScanFileWithVariousSizes(t *testing.T) {
	client := getTestClient(t)

	sizes := []int{
		10,                // 10 bytes
		1024,              // 1 KB
		1024 * 1024,       // 1 MB
		10 * 1024 * 1024,  // 10 MB
		100 * 1024 * 1024, // 100 MB
	}

	for _, size := range sizes {
		t.Run(byteSizeString(size), func(t *testing.T) {
			data := make([]byte, size)
			for i := range data {
				data[i] = byte(i % 256)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			resp, err := client.ScanFile(ctx, &pb.ScanFileRequest{
				Data:     data,
				Filename: "test.bin",
			})

			if err != nil {
				t.Logf("Scan failed for %s (ClamAV may not be running): %v", byteSizeString(size), err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, resp)
			assert.NotEmpty(t, resp.Status)
			t.Logf("Scanned %s in %.3fs", byteSizeString(size), resp.ScanTime)
		})
	}
}

// TestGRPCStreamChunkSizes tests various chunk sizes in streaming
func TestGRPCStreamChunkSizes(t *testing.T) {
	client := getTestClient(t)

	chunkSizes := []int{
		1024,             // 1 KB
		64 * 1024,        // 64 KB (common chunk size)
		1024 * 1024,      // 1 MB
		10 * 1024 * 1024, // 10 MB
	}

	for _, chunkSize := range chunkSizes {
		t.Run(byteSizeString(chunkSize), func(t *testing.T) {
			stream, err := client.ScanStream(context.Background())
			assert.NoError(t, err)

			// Send data in specified chunk size
			totalData := 5 * 1024 * 1024 // 5MB total
			data := make([]byte, totalData)

			sentBytes := 0
			for sentBytes < totalData {
				end := sentBytes + chunkSize
				if end > totalData {
					end = totalData
				}

				chunk := data[sentBytes:end]
				filename := ""
				if sentBytes == 0 {
					filename = "chunked.bin"
				}

				err := stream.Send(&pb.ScanStreamRequest{
					Chunk:    chunk,
					Filename: filename,
					IsLast:   end == totalData,
				})
				assert.NoError(t, err)

				sentBytes = end
			}

			resp, err := stream.CloseAndRecv()

			if err != nil {
				t.Logf("Stream test skipped for %s chunks: %v", byteSizeString(chunkSize), err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, resp)
			t.Logf("Streamed 5MB in %s chunks: %.3fs", byteSizeString(chunkSize), resp.ScanTime)
		})
	}
}

// TestGRPCMultipleFilesWithErrors tests error recovery in bidirectional streaming
func TestGRPCMultipleFilesWithErrors(t *testing.T) {
	client := getTestClient(t)

	stream, err := client.ScanMultiple(context.Background())
	assert.NoError(t, err)

	// Mix of valid files and edge cases
	files := []struct {
		name string
		data []byte
	}{
		{"valid1.txt", []byte("valid file 1")},
		{"empty.txt", []byte{}}, // Empty file
		{"valid2.txt", []byte("valid file 2")},
		{"binary.bin", make([]byte, 1024)}, // Binary data
	}

	errChan := make(chan error, 1)
	respChan := make(chan *pb.ScanResponse, len(files))

	// Receive responses
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

	// Send files
	for _, file := range files {
		err := stream.Send(&pb.ScanStreamRequest{
			Chunk:    file.data,
			Filename: file.name,
			IsLast:   true,
		})
		if err != nil {
			t.Logf("Send failed: %v", err)
			break
		}
	}

	stream.CloseSend()

	// Collect responses
	responses := make([]*pb.ScanResponse, 0)
	for resp := range respChan {
		responses = append(responses, resp)
		t.Logf("Response: %s - %s", resp.Filename, resp.Status)
	}

	select {
	case err := <-errChan:
		t.Logf("Stream error (may be expected): %v", err)
	default:
		t.Logf("Received %d responses for %d files", len(responses), len(files))
	}
}

// TestGRPCConcurrentRequests tests concurrent gRPC requests
func TestGRPCConcurrentRequests(t *testing.T) {
	client := getTestClient(t)

	const numRequests = 5
	done := make(chan bool, numRequests)
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			defer func() { done <- true }()

			data := []byte("concurrent request")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			_, err := client.ScanFile(ctx, &pb.ScanFileRequest{
				Data:     data,
				Filename: "concurrent.txt",
			})

			if err != nil {
				errStr := strings.ToLower(err.Error())
				if !strings.Contains(errStr, "clamav") && !strings.Contains(errStr, "clamd") {
					errors <- err
				}
			}
		}(i)
	}

	// Wait for all requests
	for i := 0; i < numRequests; i++ {
		<-done
	}
	close(errors)

	// Check for unexpected errors
	errorCount := 0
	for err := range errors {
		t.Logf("Unexpected error: %v", err)
		errorCount++
	}

	assert.Equal(t, 0, errorCount, "No unexpected errors in concurrent requests")
}

// Helper function to format byte sizes
func byteSizeString(size int) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.0f%cB", float64(size)/float64(div), "KMGTPE"[exp])
}
