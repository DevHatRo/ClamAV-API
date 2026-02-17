package main

import (
	"fmt"
	"io"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func handleScan(c *gin.Context) {
	logger := GetLogger()

	// Get the uploaded file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		logger.Warn("File upload failed",
			zap.String("client_ip", c.ClientIP()),
			zap.Error(err))
		c.JSON(400, gin.H{
			"message": "Provide a single file",
		})
		return
	}
	defer file.Close()

	// Check file size against maximum allowed
	if header.Size > config.MaxContentLength {
		logger.Warn("Scan rejected: file too large",
			zap.String("filename", header.Filename),
			zap.Int64("file_size", header.Size),
			zap.Int64("max_allowed", config.MaxContentLength),
			zap.String("client_ip", c.ClientIP()))
		c.JSON(413, gin.H{
			"message": fmt.Sprintf("File too large. Maximum size is %d bytes", config.MaxContentLength),
		})
		return
	}

	logger.Debug("File received for scanning",
		zap.String("filename", header.Filename),
		zap.Int64("size", header.Size),
		zap.String("client_ip", c.ClientIP()))

	scansInProgress.Inc()
	result, scanErr := performScan(c.Request.Context(), file, config.ScanTimeout)
	scansInProgress.Dec()
	recordScanMetrics("rest_scan", result, scanErr)

	if scanErr != nil {
		respondScanError(c, logger, scanErr, header.Filename)
		return
	}

	logger.Info("Scan completed",
		zap.String("filename", header.Filename),
		zap.String("status", result.Status),
		zap.String("result", result.Description),
		zap.Float64("elapsed_seconds", result.ScanTime),
		zap.String("client_ip", c.ClientIP()))

	c.JSON(200, gin.H{
		"status":  result.Status,
		"message": result.Description,
		"time":    result.ScanTime,
	})
}

func handleStreamScan(c *gin.Context) {
	logger := GetLogger()

	// Validate request before doing any work
	contentLength := c.Request.ContentLength
	if contentLength <= 0 {
		logger.Warn("Stream scan rejected: missing or invalid Content-Length",
			zap.Int64("content_length", contentLength),
			zap.String("client_ip", c.ClientIP()))
		c.JSON(400, gin.H{
			"message": "Content-Length header is required and must be greater than 0",
		})
		return
	}
	if contentLength > config.MaxContentLength {
		logger.Warn("Stream scan rejected: file too large",
			zap.Int64("content_length", contentLength),
			zap.Int64("max_allowed", config.MaxContentLength),
			zap.String("client_ip", c.ClientIP()))
		c.JSON(400, gin.H{
			"message": fmt.Sprintf("File too large. Maximum size is %d bytes", config.MaxContentLength),
		})
		return
	}

	logger.Debug("Stream scan started",
		zap.Int64("content_length", contentLength),
		zap.String("client_ip", c.ClientIP()))

	// Wrap body with a LimitedReader to enforce size limit
	body := c.Request.Body
	defer body.Close()
	limitedReader := &io.LimitedReader{
		R: body,
		N: config.MaxContentLength,
	}

	scansInProgress.Inc()
	result, scanErr := performScan(c.Request.Context(), limitedReader, config.ScanTimeout)
	scansInProgress.Dec()
	recordScanMetrics("rest_stream_scan", result, scanErr)

	if scanErr != nil {
		respondScanError(c, logger, scanErr, "stream")
		return
	}

	logger.Info("Stream scan completed",
		zap.String("status", result.Status),
		zap.String("result", result.Description),
		zap.Int64("content_length", contentLength),
		zap.Float64("elapsed_seconds", result.ScanTime),
		zap.String("client_ip", c.ClientIP()))

	c.JSON(200, gin.H{
		"status":  result.Status,
		"message": result.Description,
		"time":    result.ScanTime,
	})
}

// respondScanError maps scan errors to appropriate HTTP responses
func respondScanError(c *gin.Context, logger *zap.Logger, err error, filename string) {
	switch e := err.(type) {
	case *ScanTimeoutError:
		logger.Warn("Scan timeout",
			zap.String("filename", filename),
			zap.Float64("timeout_seconds", e.Timeout.Seconds()))
		c.JSON(504, gin.H{
			"status":  "Scan timeout",
			"message": e.Error(),
		})
	case *ScanEngineError:
		logger.Error("Scan error",
			zap.String("filename", filename),
			zap.String("error", e.Description))
		c.JSON(502, gin.H{
			"status":  "Clamd service down",
			"message": e.Description,
		})
	default:
		logger.Error("Scan failed",
			zap.String("filename", filename),
			zap.Error(err))
		c.JSON(502, gin.H{
			"status":  "Clamd service down",
			"message": err.Error(),
		})
	}
}

func handleHealthCheck(c *gin.Context) {
	logger := GetLogger()

	// Single ping to check ClamAV availability
	if err := pingClamd(); err != nil {
		healthCheckStatus.Set(0)
		logger.Warn("Health check failed", zap.Error(err))
		c.JSON(502, gin.H{
			"message": "Clamd service unavailable",
		})
		return
	}

	healthCheckStatus.Set(1)
	logger.Debug("Health check passed")
	c.JSON(200, gin.H{
		"message": "ok",
	})
}

func handleVersion(c *gin.Context) {
	c.JSON(200, gin.H{
		"version": Version,
		"commit":  CommitHash,
		"build":   BuildTime,
	})
}
