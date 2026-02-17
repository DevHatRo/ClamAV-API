package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	pb "clamav-api/proto"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCServer implements the ClamAV gRPC service
type GRPCServer struct {
	pb.UnimplementedClamAVScannerServer
	config *Config
}

// NewGRPCServer creates a new gRPC server instance with the given config
func NewGRPCServer(cfg *Config) *GRPCServer {
	return &GRPCServer{config: cfg}
}

// HealthCheck implements the health check RPC
func (s *GRPCServer) HealthCheck(ctx context.Context, req *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	logger := GetLogger()

	// Single ping to check ClamAV availability
	if err := pingClamd(); err != nil {
		logger.Warn("gRPC health check failed", zap.Error(err))
		return &pb.HealthCheckResponse{
			Status:  "unhealthy",
			Message: fmt.Sprintf("ClamAV service unavailable: %v", err),
		}, nil
	}

	logger.Debug("gRPC health check passed")
	return &pb.HealthCheckResponse{
		Status:  "healthy",
		Message: "ok",
	}, nil
}

// ScanFile implements the unary scan RPC
func (s *GRPCServer) ScanFile(ctx context.Context, req *pb.ScanFileRequest) (*pb.ScanResponse, error) {
	logger := GetLogger()

	// Validate request
	if len(req.Data) == 0 {
		logger.Warn("gRPC scan rejected: empty file data")
		return nil, status.Error(codes.InvalidArgument, "file data is required")
	}

	dataSize := int64(len(req.Data))
	if dataSize > s.config.MaxContentLength {
		logger.Warn("gRPC scan rejected: file too large",
			zap.Int64("size", dataSize),
			zap.Int64("max_allowed", s.config.MaxContentLength),
			zap.String("filename", req.Filename))
		return nil, status.Errorf(codes.InvalidArgument, "file too large, maximum size is %d bytes", s.config.MaxContentLength)
	}

	logger.Debug("gRPC scan started",
		zap.String("filename", req.Filename),
		zap.Int64("size", dataSize))

	// Get the shared ClamAV client
	clam := getClamdClient()

	// Scan the file
	startTime := time.Now()
	reader := bytes.NewReader(req.Data)

	done := make(chan bool)
	defer close(done)

	response, scanErr := clam.ScanStream(reader, done)
	if scanErr != nil {
		return nil, status.Errorf(codes.Internal, "scan failed: %v", scanErr)
	}

	// Process scan results with timeout
	select {
	case result := <-response:
		elapsed := time.Since(startTime).Seconds()

		if result.Status == "ERROR" {
			logger.Error("gRPC scan error",
				zap.String("filename", req.Filename),
				zap.String("error", result.Description),
				zap.Float64("elapsed_seconds", elapsed))
			return nil, status.Errorf(codes.Internal, "scan error: %s", result.Description)
		}

		logger.Info("gRPC scan completed",
			zap.String("filename", req.Filename),
			zap.String("status", result.Status),
			zap.String("result", result.Description),
			zap.Float64("elapsed_seconds", elapsed))

		return &pb.ScanResponse{
			Status:   result.Status,
			Message:  result.Description,
			ScanTime: elapsed,
			Filename: req.Filename,
		}, nil

	case <-time.After(s.config.ScanTimeout):
		logger.Warn("gRPC scan timeout",
			zap.String("filename", req.Filename),
			zap.Float64("timeout_seconds", s.config.ScanTimeout.Seconds()))
		return nil, status.Errorf(codes.DeadlineExceeded, "scan operation timed out after %.0f seconds", s.config.ScanTimeout.Seconds())

	case <-ctx.Done():
		logger.Info("gRPC scan canceled", zap.String("filename", req.Filename))
		return nil, status.Error(codes.Canceled, "request canceled by client")
	}
}

// ScanStream implements the client streaming scan RPC
func (s *GRPCServer) ScanStream(stream pb.ClamAVScanner_ScanStreamServer) error {
	logger := GetLogger()
	var buffer bytes.Buffer
	var filename string
	var totalSize int64

	// Receive chunks from client
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Error("Failed to receive chunk", zap.Error(err))
			return status.Errorf(codes.Internal, "failed to receive chunk: %v", err)
		}

		if filename == "" && req.Filename != "" {
			filename = req.Filename
			logger.Debug("gRPC stream scan started", zap.String("filename", filename))
		}

		chunkSize := int64(len(req.Chunk))
		if totalSize+chunkSize > s.config.MaxContentLength {
			logger.Warn("gRPC stream scan rejected: file too large",
				zap.String("filename", filename),
				zap.Int64("total_size", totalSize+chunkSize),
				zap.Int64("max_allowed", s.config.MaxContentLength))
			return status.Errorf(codes.InvalidArgument, "file too large, maximum size is %d bytes", s.config.MaxContentLength)
		}

		if _, err := buffer.Write(req.Chunk); err != nil {
			return status.Errorf(codes.Internal, "failed to write chunk: %v", err)
		}

		totalSize += chunkSize

		if req.IsLast {
			break
		}
	}

	clam := getClamdClient()

	startTime := time.Now()
	reader := bytes.NewReader(buffer.Bytes())

	done := make(chan bool)
	defer close(done)

	response, scanErr := clam.ScanStream(reader, done)
	if scanErr != nil {
		return status.Errorf(codes.Internal, "scan failed: %v", scanErr)
	}

	ctx := stream.Context()
	select {
	case result := <-response:
		elapsed := time.Since(startTime).Seconds()

		if result.Status == "ERROR" {
			return status.Errorf(codes.Internal, "scan error: %s", result.Description)
		}

		return stream.SendAndClose(&pb.ScanResponse{
			Status:   result.Status,
			Message:  result.Description,
			ScanTime: elapsed,
			Filename: filename,
		})

	case <-time.After(s.config.ScanTimeout):
		return status.Errorf(codes.DeadlineExceeded, "scan operation timed out after %.0f seconds", s.config.ScanTimeout.Seconds())

	case <-ctx.Done():
		return status.Error(codes.Canceled, "request canceled by client")
	}
}

// ScanMultiple implements the bidirectional streaming scan RPC
func (s *GRPCServer) ScanMultiple(stream pb.ClamAVScanner_ScanMultipleServer) error {
	var buffer bytes.Buffer
	var filename string
	var totalSize int64

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Internal, "failed to receive chunk: %v", err)
		}

		chunkSize := int64(len(req.Chunk))
		if totalSize+chunkSize > s.config.MaxContentLength {
			return status.Errorf(codes.InvalidArgument, "file too large, maximum size is %d bytes", s.config.MaxContentLength)
		}

		if filename == "" && req.Filename != "" {
			filename = req.Filename
		}

		if _, err := buffer.Write(req.Chunk); err != nil {
			return status.Errorf(codes.Internal, "failed to write chunk: %v", err)
		}

		totalSize += chunkSize

		if req.IsLast {
			response, err := s.scanData(&buffer, filename, stream.Context())
			if err != nil {
				if err := stream.Send(&pb.ScanResponse{
					Status:   "ERROR",
					Message:  err.Error(),
					Filename: filename,
				}); err != nil {
					return err
				}
			} else {
				if err := stream.Send(response); err != nil {
					return err
				}
			}

			buffer.Reset()
			filename = ""
			totalSize = 0
		}
	}
}

// scanData is a helper function to scan data from a buffer
func (s *GRPCServer) scanData(buffer *bytes.Buffer, filename string, ctx context.Context) (*pb.ScanResponse, error) {
	clam := getClamdClient()

	startTime := time.Now()
	reader := bytes.NewReader(buffer.Bytes())

	done := make(chan bool)
	defer close(done)

	response, scanErr := clam.ScanStream(reader, done)
	if scanErr != nil {
		return nil, fmt.Errorf("scan failed: %v", scanErr)
	}

	select {
	case result := <-response:
		elapsed := time.Since(startTime).Seconds()

		if result.Status == "ERROR" {
			return nil, fmt.Errorf("scan error: %s", result.Description)
		}

		return &pb.ScanResponse{
			Status:   result.Status,
			Message:  result.Description,
			ScanTime: elapsed,
			Filename: filename,
		}, nil

	case <-time.After(s.config.ScanTimeout):
		return nil, fmt.Errorf("scan operation timed out after %.0f seconds", s.config.ScanTimeout.Seconds())

	case <-ctx.Done():
		return nil, fmt.Errorf("request canceled by client")
	}
}
