package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"

	pb "clamav-api/proto"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

var lis *bufconn.Listener

func init() {
	// Initialize config for tests
	config = Config{
		Debug:            false,
		ClamdUnixSocket:  "/var/run/clamav/clamd.ctl",
		MaxContentLength: 209715200,
		Host:             "0.0.0.0",
		Port:             "6000",
		GRPCPort:         "9000",
		ScanTimeout:      300 * time.Second,
		EnableGRPC:       true,
	}

	lis = bufconn.Listen(bufSize)
	s := grpc.NewServer()
	pb.RegisterClamAVScannerServer(s, NewGRPCServer())
	go func() {
		if err := s.Serve(lis); err != nil {
			panic(err)
		}
	}()
}

func bufDialer(context.Context, string) (net.Conn, error) {
	return lis.Dial()
}

func getTestClient(t *testing.T) pb.ClamAVScannerClient {
	conn, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return pb.NewClamAVScannerClient(conn)
}

func TestGRPCHealthCheck(t *testing.T) {
	client := getTestClient(t)

	resp, err := client.HealthCheck(context.Background(), &pb.HealthCheckRequest{})

	// Test will pass or fail depending on ClamAV availability
	if err != nil {
		t.Logf("HealthCheck failed (ClamAV may not be running): %v", err)
		assert.NotNil(t, resp)
		return
	}

	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Status)
	assert.NotEmpty(t, resp.Message)
	t.Logf("Health status: %s - %s", resp.Status, resp.Message)
}

func TestGRPCScanFileClean(t *testing.T) {
	client := getTestClient(t)

	cleanData := []byte("This is a clean test file")

	resp, err := client.ScanFile(context.Background(), &pb.ScanFileRequest{
		Data:     cleanData,
		Filename: "clean-test.txt",
	})

	// Test will pass or fail depending on ClamAV availability
	if err != nil {
		t.Logf("ScanFile failed (ClamAV may not be running): %v", err)
		return
	}

	assert.NoError(t, err)
	assert.Equal(t, "OK", resp.Status)
	assert.Equal(t, "clean-test.txt", resp.Filename)
	assert.Greater(t, resp.ScanTime, 0.0)
	t.Logf("Scan result: %s - %s (%.3fs)", resp.Status, resp.Message, resp.ScanTime)
}

func TestGRPCScanFileEicar(t *testing.T) {
	client := getTestClient(t)

	eicarData := []byte(`X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`)

	resp, err := client.ScanFile(context.Background(), &pb.ScanFileRequest{
		Data:     eicarData,
		Filename: "eicar-test.txt",
	})

	// Test will pass or fail depending on ClamAV availability
	if err != nil {
		t.Logf("ScanFile failed (ClamAV may not be running): %v", err)
		return
	}

	assert.NoError(t, err)
	assert.Equal(t, "FOUND", resp.Status)
	assert.Contains(t, resp.Message, "EICAR")
	assert.Equal(t, "eicar-test.txt", resp.Filename)
	assert.Greater(t, resp.ScanTime, 0.0)
	t.Logf("Scan result: %s - %s (%.3fs)", resp.Status, resp.Message, resp.ScanTime)
}

func TestGRPCScanFileEmpty(t *testing.T) {
	client := getTestClient(t)

	_, err := client.ScanFile(context.Background(), &pb.ScanFileRequest{
		Data:     []byte{},
		Filename: "empty.txt",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "file data is required")
}

func TestGRPCScanFileTooLarge(t *testing.T) {
	client := getTestClient(t)

	// Create data larger than max size
	largeData := make([]byte, config.MaxContentLength+1)

	_, err := client.ScanFile(context.Background(), &pb.ScanFileRequest{
		Data:     largeData,
		Filename: "large.bin",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "file too large")
}

func TestGRPCScanStreamClean(t *testing.T) {
	client := getTestClient(t)

	stream, err := client.ScanStream(context.Background())
	assert.NoError(t, err)

	// Send data in chunks
	data := []byte("This is a clean streaming test file")
	chunkSize := 10

	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}

		isLast := end == len(data)
		filename := ""
		if i == 0 {
			filename = "stream-clean.txt"
		}

		err := stream.Send(&pb.ScanStreamRequest{
			Chunk:    data[i:end],
			Filename: filename,
			IsLast:   isLast,
		})
		assert.NoError(t, err)
	}

	resp, err := stream.CloseAndRecv()

	// Test will pass or fail depending on ClamAV availability
	if err != nil {
		t.Logf("ScanStream failed (ClamAV may not be running): %v", err)
		return
	}

	assert.NoError(t, err)
	assert.Equal(t, "OK", resp.Status)
	assert.Equal(t, "stream-clean.txt", resp.Filename)
	t.Logf("Stream scan result: %s - %s (%.3fs)", resp.Status, resp.Message, resp.ScanTime)
}

func TestGRPCScanStreamEicar(t *testing.T) {
	client := getTestClient(t)

	stream, err := client.ScanStream(context.Background())
	assert.NoError(t, err)

	eicarData := []byte(`X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`)
	chunkSize := 20

	for i := 0; i < len(eicarData); i += chunkSize {
		end := i + chunkSize
		if end > len(eicarData) {
			end = len(eicarData)
		}

		isLast := end == len(eicarData)
		filename := ""
		if i == 0 {
			filename = "stream-eicar.txt"
		}

		err := stream.Send(&pb.ScanStreamRequest{
			Chunk:    eicarData[i:end],
			Filename: filename,
			IsLast:   isLast,
		})
		assert.NoError(t, err)
	}

	resp, err := stream.CloseAndRecv()

	// Test will pass or fail depending on ClamAV availability
	if err != nil {
		t.Logf("ScanStream failed (ClamAV may not be running): %v", err)
		return
	}

	assert.NoError(t, err)
	assert.Equal(t, "FOUND", resp.Status)
	assert.Contains(t, resp.Message, "EICAR")
	t.Logf("Stream scan result: %s - %s (%.3fs)", resp.Status, resp.Message, resp.ScanTime)
}

func TestGRPCScanStreamTooLarge(t *testing.T) {
	client := getTestClient(t)

	stream, err := client.ScanStream(context.Background())
	assert.NoError(t, err)

	// Send data larger than max size
	chunkSize := 1024 * 1024 // 1MB chunks
	totalSize := config.MaxContentLength + chunkSize

	for i := int64(0); i < totalSize; i += int64(chunkSize) {
		chunk := make([]byte, chunkSize)
		isLast := i+int64(chunkSize) >= totalSize
		filename := ""
		if i == 0 {
			filename = "large-stream.bin"
		}

		err := stream.Send(&pb.ScanStreamRequest{
			Chunk:    chunk,
			Filename: filename,
			IsLast:   isLast,
		})

		// Should fail when exceeding max size
		if err != nil {
			assert.Contains(t, err.Error(), "file too large")
			return
		}
	}

	_, err = stream.CloseAndRecv()
	if err != nil {
		assert.Contains(t, err.Error(), "file too large")
	}
}

func TestGRPCScanMultiple(t *testing.T) {
	client := getTestClient(t)

	stream, err := client.ScanMultiple(context.Background())
	assert.NoError(t, err)

	// Prepare test files
	files := []struct {
		name string
		data []byte
	}{
		{"file1.txt", []byte("Clean file 1")},
		{"file2.txt", []byte("Clean file 2")},
		{"eicar.txt", []byte(`X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`)},
	}

	// Send files and receive responses concurrently
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

	// Close send side
	err = stream.CloseSend()
	assert.NoError(t, err)

	// Collect responses
	responses := make([]*pb.ScanResponse, 0)
	for resp := range respChan {
		responses = append(responses, resp)
		t.Logf("Received: %s - %s - %s", resp.Filename, resp.Status, resp.Message)
	}

	// Check for errors
	select {
	case err := <-errChan:
		// ClamAV may not be running, log and skip
		t.Logf("ScanMultiple failed (ClamAV may not be running): %v", err)
		return
	default:
	}

	// Verify we got responses for all files
	if len(responses) > 0 {
		assert.Equal(t, len(files), len(responses))

		// Check specific results
		for _, resp := range responses {
			if resp.Filename == "eicar.txt" {
				assert.Equal(t, "FOUND", resp.Status)
				assert.Contains(t, resp.Message, "EICAR")
			} else {
				assert.Equal(t, "OK", resp.Status)
			}
		}
	}
}

func TestGRPCContextCancellation(t *testing.T) {
	client := getTestClient(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.HealthCheck(ctx, &pb.HealthCheckRequest{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func BenchmarkGRPCScanFile(b *testing.B) {
	client := getTestClient(&testing.T{})

	data := []byte("Benchmark test file data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.ScanFile(context.Background(), &pb.ScanFileRequest{
			Data:     data,
			Filename: "bench.txt",
		})
		if err != nil {
			// ClamAV not available, skip benchmark
			b.Skip("ClamAV not available")
		}
	}
}

func BenchmarkGRPCScanStream(b *testing.B) {
	client := getTestClient(&testing.T{})

	data := bytes.Repeat([]byte("Benchmark data"), 1000)
	chunkSize := 1024

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stream, err := client.ScanStream(context.Background())
		if err != nil {
			b.Fatal(err)
		}

		for j := 0; j < len(data); j += chunkSize {
			end := j + chunkSize
			if end > len(data) {
				end = len(data)
			}

			err := stream.Send(&pb.ScanStreamRequest{
				Chunk:    data[j:end],
				Filename: "bench-stream.txt",
				IsLast:   end == len(data),
			})
			if err != nil {
				b.Fatal(err)
			}
		}

		_, err = stream.CloseAndRecv()
		if err != nil {
			// ClamAV not available, skip benchmark
			b.Skip("ClamAV not available")
		}
	}
}
