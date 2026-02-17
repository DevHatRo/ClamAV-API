package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "clamav-api/proto"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	// Parse configuration
	parseConfig()

	// Ensure logger is synced on exit
	defer SyncLogger()

	logger := GetLogger()

	// Initialize ClamAV client (reused across all requests)
	getClamdClient()
	logger.Info("ClamAV client initialized", zap.String("socket", config.ClamdUnixSocket))

	// Create error channel
	errChan := make(chan error, 2)

	// Start gRPC server if enabled
	var grpcSrv *grpc.Server
	if config.EnableGRPC {
		grpcSrv = startGRPCServer(errChan)
	}

	// Start REST API server
	httpSrv := startRESTServer(errChan)

	// Wait for interrupt signal or error
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	var serverErr error
	select {
	case serverErr = <-errChan:
		logger.Error("Server error, initiating shutdown", zap.Error(serverErr))
	case sig := <-sigChan:
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
	}

	// Graceful shutdown with timeout
	logger.Info("Initiating graceful shutdown...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Shut down REST server
	if httpSrv != nil {
		logger.Info("Shutting down REST server...")
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			logger.Error("REST server forced to shutdown", zap.Error(err))
		}
	}

	// Shut down gRPC server
	if grpcSrv != nil {
		logger.Info("Shutting down gRPC server...")
		stopped := make(chan struct{})
		go func() {
			grpcSrv.GracefulStop()
			close(stopped)
		}()

		select {
		case <-stopped:
			logger.Info("gRPC server stopped gracefully")
		case <-shutdownCtx.Done():
			logger.Warn("gRPC graceful shutdown timed out, forcing stop")
			grpcSrv.Stop()
		}
	}

	logger.Info("All servers stopped")

	if serverErr != nil {
		os.Exit(1)
	}
}

func startRESTServer(errChan chan<- error) *http.Server {
	logger := GetLogger()

	// Initialize router
	router := gin.Default()
	router.Use(metricsMiddleware())

	// Set maximum multipart memory
	router.MaxMultipartMemory = config.MaxContentLength

	// Register routes
	router.POST("/api/scan", handleScan)
	router.POST("/api/stream-scan", handleStreamScan)
	router.GET("/api/health-check", handleHealthCheck)
	router.GET("/api/version", handleVersion)
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Create HTTP server
	addr := fmt.Sprintf("%s:%s", config.Host, config.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	logger.Info("Starting REST API server", zap.String("address", addr))
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("REST server error", zap.Error(err))
			errChan <- fmt.Errorf("REST server error: %w", err)
		}
	}()

	return srv
}

func startGRPCServer(errChan chan<- error) *grpc.Server {
	logger := GetLogger()

	// Create TCP listener
	addr := fmt.Sprintf("%s:%s", config.Host, config.GRPCPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("Failed to create gRPC listener",
			zap.String("address", addr),
			zap.Error(err))
		errChan <- fmt.Errorf("failed to listen on %s: %w", addr, err)
		return nil
	}

	// Create gRPC server with options
	maxMsgSize := int(config.MaxContentLength)
	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(maxMsgSize),
		grpc.MaxSendMsgSize(maxMsgSize),
	)

	// Register service
	pb.RegisterClamAVScannerServer(grpcServer, NewGRPCServer(&config))

	// Only enable reflection in debug mode (exposes service schema)
	if config.Debug {
		reflection.Register(grpcServer)
		logger.Info("gRPC reflection enabled (debug mode)")
	}

	logger.Info("Starting gRPC server",
		zap.String("address", addr),
		zap.Int("max_message_size", maxMsgSize))

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			logger.Error("gRPC server error", zap.Error(err))
			errChan <- fmt.Errorf("gRPC server error: %w", err)
		}
	}()

	return grpcServer
}
