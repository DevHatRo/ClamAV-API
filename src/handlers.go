package main

import (
	"fmt"
	"io"
	"time"

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

	// Get the shared ClamAV client
	clam := getClamdClient()

	// Scan the file
	startTime := time.Now()

	// Create done channel for scan
	done := make(chan bool)
	defer close(done) // Ensure channel is closed to prevent leaks

	response, scanErr := clam.ScanStream(file, done)
	if scanErr != nil {
		logger.Error("Scan stream failed",
			zap.String("filename", header.Filename),
			zap.Error(scanErr))
		c.JSON(502, gin.H{
			"status":  "Clamd service down",
			"message": scanErr.Error(),
		})
		return
	}

	// Process scan results with timeout
	select {
	case result := <-response:
		elapsed := time.Since(startTime).Seconds()

		if result.Status == "ERROR" {
			logger.Error("Scan error",
				zap.String("filename", header.Filename),
				zap.String("error", result.Description),
				zap.Float64("elapsed_seconds", elapsed))
			c.JSON(502, gin.H{
				"status":  "Clamd service down",
				"message": result.Description,
			})
			return
		}

		logger.Info("Scan completed",
			zap.String("filename", header.Filename),
			zap.String("status", result.Status),
			zap.String("result", result.Description),
			zap.Float64("elapsed_seconds", elapsed),
			zap.String("client_ip", c.ClientIP()))

		c.JSON(200, gin.H{
			"status":  result.Status,
			"message": result.Description,
			"time":    elapsed,
		})
	case <-time.After(config.ScanTimeout):
		logger.Warn("Scan timeout",
			zap.String("filename", header.Filename),
			zap.Float64("timeout_seconds", config.ScanTimeout.Seconds()))
		// Drain the response channel so ScanStream's goroutine can exit
		go func() {
			for range response {
			}
		}()
		c.JSON(504, gin.H{
			"status":  "Scan timeout",
			"message": fmt.Sprintf("Scan operation timed out after %.0f seconds", config.ScanTimeout.Seconds()),
		})
	}
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

	// Get the shared ClamAV client
	clam := getClamdClient()

	// Scan the stream
	startTime := time.Now()

	// Create done channel for scan
	done := make(chan bool)
	defer close(done) // Ensure channel is closed to prevent leaks

	response, scanErr := clam.ScanStream(limitedReader, done)
	if scanErr != nil {
		logger.Error("Stream scan failed", zap.Error(scanErr))
		c.JSON(502, gin.H{
			"status":  "Clamd service down",
			"message": scanErr.Error(),
		})
		return
	}

	// Process scan results with timeout
	select {
	case result := <-response:
		elapsed := time.Since(startTime).Seconds()

		if result.Status == "ERROR" {
			logger.Error("Stream scan error",
				zap.String("error", result.Description),
				zap.Float64("elapsed_seconds", elapsed))
			c.JSON(502, gin.H{
				"status":  "Clamd service down",
				"message": result.Description,
			})
			return
		}

		logger.Info("Stream scan completed",
			zap.String("status", result.Status),
			zap.String("result", result.Description),
			zap.Int64("content_length", contentLength),
			zap.Float64("elapsed_seconds", elapsed),
			zap.String("client_ip", c.ClientIP()))

		c.JSON(200, gin.H{
			"status":  result.Status,
			"message": result.Description,
			"time":    elapsed,
		})
	case <-time.After(config.ScanTimeout):
		logger.Warn("Stream scan timeout",
			zap.Int64("content_length", contentLength),
			zap.Float64("timeout_seconds", config.ScanTimeout.Seconds()))
		// Drain the response channel so ScanStream's goroutine can exit
		go func() {
			for range response {
			}
		}()
		c.JSON(504, gin.H{
			"status":  "Scan timeout",
			"message": fmt.Sprintf("Scan operation timed out after %.0f seconds", config.ScanTimeout.Seconds()),
		})
	}
}

func handleHealthCheck(c *gin.Context) {
	logger := GetLogger()

	// Single ping to check ClamAV availability
	if err := pingClamd(); err != nil {
		logger.Warn("Health check failed", zap.Error(err))
		c.JSON(502, gin.H{
			"message": "Clamd service unavailable",
		})
		return
	}

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
