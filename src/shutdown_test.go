package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	pb "clamav-api/proto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func getFreePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return fmt.Sprintf("%d", port)
}

func TestRESTServerGracefulShutdown(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origHost := config.Host
	origPort := config.Port
	config.Host = "127.0.0.1"
	config.Port = getFreePort(t)
	defer func() {
		config.Host = origHost
		config.Port = origPort
	}()

	errChan := make(chan error, 1)
	srv := startRESTServer(errChan)
	assert.NotNil(t, srv)

	// Give the server time to start
	time.Sleep(100 * time.Millisecond)

	// Verify server is accepting connections (health check may return 502 without ClamAV, but connection should succeed)
	addr := fmt.Sprintf("http://127.0.0.1:%s/api/health-check", config.Port)
	resp, err := http.Get(addr)
	assert.NoError(t, err, "Server should accept connections before shutdown")
	if resp != nil {
		resp.Body.Close()
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.Shutdown(ctx)
	assert.NoError(t, err, "Shutdown should complete without error")

	// Verify server stopped accepting connections
	_, err = http.Get(addr)
	assert.Error(t, err, "Server should refuse connections after shutdown")

	// Verify no unexpected server errors
	select {
	case err := <-errChan:
		t.Fatalf("Unexpected server error: %v", err)
	default:
	}
}

func TestGRPCServerGracefulShutdown(t *testing.T) {
	origHost := config.Host
	origGRPCPort := config.GRPCPort
	config.Host = "127.0.0.1"
	config.GRPCPort = getFreePort(t)
	defer func() {
		config.Host = origHost
		config.GRPCPort = origGRPCPort
	}()

	errChan := make(chan error, 1)
	srv := startGRPCServer(errChan)
	assert.NotNil(t, srv)

	// Give the server time to start
	time.Sleep(100 * time.Millisecond)

	// Verify server is accepting connections (NewClient connects lazily; the RPC call establishes it)
	addr := fmt.Sprintf("127.0.0.1:%s", config.GRPCPort)
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	assert.NoError(t, err, "Should create gRPC client")

	// Verify the server responds (health check returns response even without ClamAV)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := pb.NewClamAVScannerClient(conn)
	resp, err := client.HealthCheck(ctx, &pb.HealthCheckRequest{})
	assert.NoError(t, err, "HealthCheck RPC should not return error")
	assert.NotNil(t, resp)
	conn.Close()

	// Graceful stop
	srv.GracefulStop()

	// Verify no unexpected server errors
	select {
	case err := <-errChan:
		t.Fatalf("Unexpected server error: %v", err)
	default:
	}
}

func TestGRPCServerGracefulShutdownCompletesWithinTimeout(t *testing.T) {
	origHost := config.Host
	origGRPCPort := config.GRPCPort
	config.Host = "127.0.0.1"
	config.GRPCPort = getFreePort(t)
	defer func() {
		config.Host = origHost
		config.GRPCPort = origGRPCPort
	}()

	errChan := make(chan error, 1)
	srv := startGRPCServer(errChan)
	assert.NotNil(t, srv)

	time.Sleep(100 * time.Millisecond)

	// GracefulStop should complete quickly when no in-flight requests
	done := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		// Success: GracefulStop completed
	case <-time.After(5 * time.Second):
		srv.Stop()
		t.Fatal("GracefulStop did not complete within 5 seconds")
	}
}

func TestStartGRPCServerWithInvalidPort(t *testing.T) {
	origHost := config.Host
	origGRPCPort := config.GRPCPort
	config.Host = "127.0.0.1"
	config.GRPCPort = "99999" // Invalid port
	defer func() {
		config.Host = origHost
		config.GRPCPort = origGRPCPort
	}()

	errChan := make(chan error, 1)
	srv := startGRPCServer(errChan)
	assert.Nil(t, srv, "Server should be nil when port is invalid")

	// Should receive an error
	select {
	case err := <-errChan:
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to listen")
	case <-time.After(2 * time.Second):
		t.Fatal("Expected error for invalid port")
	}
}

func TestStartRESTServerWithInvalidPort(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origHost := config.Host
	origPort := config.Port
	config.Host = "127.0.0.1"
	config.Port = "99999" // Invalid port
	defer func() {
		config.Host = origHost
		config.Port = origPort
	}()

	errChan := make(chan error, 1)
	srv := startRESTServer(errChan)
	assert.NotNil(t, srv) // Server struct is created, but ListenAndServe will fail

	// Should receive an error from the goroutine
	select {
	case err := <-errChan:
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "REST server error")
	case <-time.After(2 * time.Second):
		t.Fatal("Expected error for invalid port")
	}
}
