package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

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
		healthCheckStatus.Set(0)
		logger.Warn("gRPC health check failed", zap.Error(err))
		return &pb.HealthCheckResponse{
			Status:  "unhealthy",
			Message: fmt.Sprintf("ClamAV service unavailable: %v", err),
		}, nil
	}

	healthCheckStatus.Set(1)
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

	reader := bytes.NewReader(req.Data)

	scansInProgress.Inc()
	defer scansInProgress.Dec()
	result, err := performScan(ctx, reader, s.config.ScanTimeout)
	recordScanMetrics("grpc_scan", result, err)

	if err != nil {
		return nil, mapScanErrorToGRPC(err)
	}

	logger.Info("gRPC scan completed",
		zap.String("filename", req.Filename),
		zap.String("status", result.Status),
		zap.String("result", result.Description),
		zap.Float64("elapsed_seconds", result.ScanTime))

	return &pb.ScanResponse{
		Status:   result.Status,
		Message:  result.Description,
		ScanTime: result.ScanTime,
		Filename: req.Filename,
	}, nil
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

	reader := bytes.NewReader(buffer.Bytes())

	scansInProgress.Inc()
	defer scansInProgress.Dec()
	result, err := performScan(stream.Context(), reader, s.config.ScanTimeout)
	recordScanMetrics("grpc_stream_scan", result, err)

	if err != nil {
		return mapScanErrorToGRPC(err)
	}

	return stream.SendAndClose(&pb.ScanResponse{
		Status:   result.Status,
		Message:  result.Description,
		ScanTime: result.ScanTime,
		Filename: filename,
	})
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
			if err := s.scanAndRespond(&buffer, filename, stream); err != nil {
				return err
			}

			buffer.Reset()
			filename = ""
			totalSize = 0
		}
	}
}

// scanAndRespond scans buffered data and sends the result on the stream.
// Using a separate method scopes the defer for scansInProgress correctly per file.
func (s *GRPCServer) scanAndRespond(buffer *bytes.Buffer, filename string, stream pb.ClamAVScanner_ScanMultipleServer) error {
	scansInProgress.Inc()
	defer scansInProgress.Dec()

	reader := bytes.NewReader(buffer.Bytes())
	result, err := performScan(stream.Context(), reader, s.config.ScanTimeout)
	recordScanMetrics("grpc_scan_multiple", result, err)

	if err != nil {
		return stream.Send(&pb.ScanResponse{
			Status:   "ERROR",
			Message:  err.Error(),
			Filename: filename,
		})
	}

	return stream.Send(&pb.ScanResponse{
		Status:   result.Status,
		Message:  result.Description,
		ScanTime: result.ScanTime,
		Filename: filename,
	})
}

// mapScanErrorToGRPC converts scan errors to appropriate gRPC status errors.
// Uses errors.As/errors.Is so wrapped errors are recognized.
func mapScanErrorToGRPC(err error) error {
	var timeoutErr *ScanTimeoutError
	var engineErr *ScanEngineError

	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled by client")
	case errors.As(err, &timeoutErr):
		return status.Error(codes.DeadlineExceeded, timeoutErr.Error())
	case errors.As(err, &engineErr):
		return status.Errorf(codes.Internal, "scan error: %s", engineErr.Error())
	default:
		return status.Errorf(codes.Internal, "scan failed: %v", err)
	}
}
