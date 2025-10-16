package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	pb "clamav-api/proto"

	"github.com/dutchcoders/go-clamd"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Config struct {
	Debug            bool
	ClamdUnixSocket  string
	MaxContentLength int64
	Host             string
	Port             string
	GRPCPort         string
	ScanTimeout      time.Duration
	EnableGRPC       bool
}

// getEnvWithDefault gets an environment variable or returns the default value
func getEnvWithDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// getEnvBoolWithDefault gets a boolean environment variable or returns the default value
func getEnvBoolWithDefault(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		boolValue, err := strconv.ParseBool(value)
		if err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// getEnvInt64WithDefault gets an int64 environment variable or returns the default value
func getEnvInt64WithDefault(key string, defaultValue int64) int64 {
	if value, exists := os.LookupEnv(key); exists {
		intValue, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			return intValue
		}
	}
	return defaultValue
}

var config = Config{
	Debug:            false,
	ClamdUnixSocket:  "/run/clamav/clamd.ctl",
	MaxContentLength: 209715200, // 200MB
	Host:             "0.0.0.0",
	Port:             "6000",
	GRPCPort:         "9000",
	ScanTimeout:      300 * time.Second, // 5 minutes
	EnableGRPC:       true,
}

func parseConfig() {
	// Command line flags
	debug := flag.Bool("debug", config.Debug, "Enable debug mode")
	socket := flag.String("socket", config.ClamdUnixSocket, "ClamAV Unix socket path")
	maxSize := flag.Int64("max-size", config.MaxContentLength, "Maximum file size in bytes")
	host := flag.String("host", config.Host, "Host to listen on")
	port := flag.String("port", config.Port, "Port to listen on")
	grpcPort := flag.String("grpc-port", config.GRPCPort, "gRPC server port")
	scanTimeout := flag.Int64("scan-timeout", int64(config.ScanTimeout.Seconds()), "Scan timeout in seconds")
	enableGRPC := flag.Bool("enable-grpc", config.EnableGRPC, "Enable gRPC server")

	// Parse flags
	flag.Parse()

	// Update config with environment variables or flags
	config.Debug = getEnvBoolWithDefault("CLAMAV_DEBUG", *debug)
	config.ClamdUnixSocket = getEnvWithDefault("CLAMAV_SOCKET", *socket)
	config.MaxContentLength = getEnvInt64WithDefault("CLAMAV_MAX_SIZE", *maxSize)
	config.Host = getEnvWithDefault("CLAMAV_HOST", *host)
	config.Port = getEnvWithDefault("CLAMAV_PORT", *port)
	config.GRPCPort = getEnvWithDefault("CLAMAV_GRPC_PORT", *grpcPort)
	config.EnableGRPC = getEnvBoolWithDefault("CLAMAV_ENABLE_GRPC", *enableGRPC)
	timeoutSeconds := getEnvInt64WithDefault("CLAMAV_SCAN_TIMEOUT", *scanTimeout)
	config.ScanTimeout = time.Duration(timeoutSeconds) * time.Second

	// Set Gin mode based on environment variables
	if mode := os.Getenv("GIN_MODE"); mode != "" {
		gin.SetMode(mode)
	} else if os.Getenv("ENV") == "production" && !config.Debug {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	// Initialize logger
	env := "development"
	if os.Getenv("ENV") == "production" {
		env = "production"
	}
	if err := InitLogger(config.Debug, env); err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	// Log configuration
	logger := GetLogger()
	logger.Info("Configuration loaded",
		zap.Bool("debug", config.Debug),
		zap.String("clamav_socket", config.ClamdUnixSocket),
		zap.Int64("max_content_length", config.MaxContentLength),
		zap.Float64("scan_timeout_seconds", config.ScanTimeout.Seconds()),
		zap.String("rest_api_address", fmt.Sprintf("%s:%s", config.Host, config.Port)),
		zap.Bool("grpc_enabled", config.EnableGRPC),
		zap.String("grpc_address", fmt.Sprintf("%s:%s", config.Host, config.GRPCPort)),
		zap.String("gin_mode", gin.Mode()),
	)
}

func getClamdClient() (*clamd.Clamd, error) {
	c := clamd.NewClamd("unix://" + config.ClamdUnixSocket)
	// Test connection
	err := c.Ping()
	return c, err
}

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

	logger.Debug("File received for scanning",
		zap.String("filename", header.Filename),
		zap.Int64("size", header.Size),
		zap.String("client_ip", c.ClientIP()))

	// Initialize ClamAV client
	clam, err := getClamdClient()
	if err != nil {
		logger.Error("ClamAV connection failed", zap.Error(err))
		c.JSON(502, gin.H{
			"status":  "Clamd service down",
			"message": err.Error(),
		})
		return
	}

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
		c.JSON(504, gin.H{
			"status":  "Scan timeout",
			"message": fmt.Sprintf("Scan operation timed out after %.0f seconds", config.ScanTimeout.Seconds()),
		})
	}
}

func handleStreamScan(c *gin.Context) {
	logger := GetLogger()

	// Initialize ClamAV client
	clam, err := getClamdClient()
	if err != nil {
		logger.Error("ClamAV connection failed", zap.Error(err))
		c.JSON(502, gin.H{
			"status":  "Clamd service down",
			"message": err.Error(),
		})
		return
	}

	// Check content length - reject if missing, -1, or too large
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
		c.JSON(504, gin.H{
			"status":  "Scan timeout",
			"message": fmt.Sprintf("Scan operation timed out after %.0f seconds", config.ScanTimeout.Seconds()),
		})
	}
}

func handleHealthCheck(c *gin.Context) {
	logger := GetLogger()

	clam, err := getClamdClient()
	if err != nil {
		logger.Warn("Health check failed: connection error", zap.Error(err))
		c.JSON(502, gin.H{
			"message": "Clamd service unavailable",
		})
		return
	}

	// Ping ClamAV
	err = clam.Ping()
	if err != nil {
		logger.Warn("Health check failed: ping error", zap.Error(err))
		c.JSON(502, gin.H{
			"message": "Clamd service down",
		})
		return
	}

	logger.Debug("Health check passed")
	c.JSON(200, gin.H{
		"message": "ok",
	})
}

func main() {
	// Parse configuration
	parseConfig()

	// Ensure logger is synced on exit
	defer SyncLogger()

	logger := GetLogger()

	// Create error channel
	errChan := make(chan error, 2)

	// Start gRPC server if enabled
	if config.EnableGRPC {
		go startGRPCServer(errChan)
	}

	// Start REST API server
	go startRESTServer(errChan)

	// Wait for interrupt signal or error
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errChan:
		logger.Fatal("Server error", zap.Error(err))
	case sig := <-sigChan:
		logger.Info("Shutting down gracefully", zap.String("signal", sig.String()))
	}
}

func startRESTServer(errChan chan<- error) {
	logger := GetLogger()

	// Initialize router
	router := gin.Default()

	// Set maximum multipart memory
	router.MaxMultipartMemory = config.MaxContentLength

	// Register routes
	router.POST("/api/scan", handleScan)
	router.POST("/api/stream-scan", handleStreamScan)
	router.GET("/api/health-check", handleHealthCheck)

	// Start server
	addr := fmt.Sprintf("%s:%s", config.Host, config.Port)
	logger.Info("Starting REST API server", zap.String("address", addr))
	if err := router.Run(addr); err != nil {
		logger.Error("REST server error", zap.Error(err))
		errChan <- fmt.Errorf("REST server error: %w", err)
	}
}

func startGRPCServer(errChan chan<- error) {
	logger := GetLogger()

	// Create TCP listener
	addr := fmt.Sprintf("%s:%s", config.Host, config.GRPCPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("Failed to create gRPC listener",
			zap.String("address", addr),
			zap.Error(err))
		errChan <- fmt.Errorf("failed to listen on %s: %w", addr, err)
		return
	}

	// Create gRPC server with options
	maxMsgSize := int(config.MaxContentLength)
	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(maxMsgSize),
		grpc.MaxSendMsgSize(maxMsgSize),
	)

	// Register service
	pb.RegisterClamAVScannerServer(grpcServer, NewGRPCServer())

	// Register reflection service for debugging
	reflection.Register(grpcServer)

	logger.Info("Starting gRPC server",
		zap.String("address", addr),
		zap.Int("max_message_size", maxMsgSize))

	if err := grpcServer.Serve(lis); err != nil {
		logger.Error("gRPC server error", zap.Error(err))
		errChan <- fmt.Errorf("gRPC server error: %w", err)
	}
}
