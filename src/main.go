package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	pb "clamav-api/proto"

	"github.com/dutchcoders/go-clamd"
	"github.com/gin-gonic/gin"
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

	// Log configuration
	log.Printf("Configuration:")
	log.Printf("Debug: %v", config.Debug)
	log.Printf("ClamAV Socket: %s", config.ClamdUnixSocket)
	log.Printf("Max Content Length: %d bytes", config.MaxContentLength)
	log.Printf("Scan Timeout: %.0f seconds", config.ScanTimeout.Seconds())
	log.Printf("REST API Address: %s:%s", config.Host, config.Port)
	log.Printf("gRPC Enabled: %v", config.EnableGRPC)
	if config.EnableGRPC {
		log.Printf("gRPC Address: %s:%s", config.Host, config.GRPCPort)
	}
	log.Printf("Gin Mode: %s", gin.Mode())
}

func getClamdClient() (*clamd.Clamd, error) {
	c := clamd.NewClamd("unix://" + config.ClamdUnixSocket)
	// Test connection
	err := c.Ping()
	return c, err
}

func handleScan(c *gin.Context) {
	// Get the uploaded file
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{
			"message": "Provide a single file",
		})
		return
	}
	defer file.Close()

	// Initialize ClamAV client
	clam, err := getClamdClient()
	if err != nil {
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
			c.JSON(502, gin.H{
				"status":  "Clamd service down",
				"message": result.Description,
			})
			return
		}

		c.JSON(200, gin.H{
			"status":  result.Status,
			"message": result.Description,
			"time":    elapsed,
		})
	case <-time.After(config.ScanTimeout):
		c.JSON(504, gin.H{
			"status":  "Scan timeout",
			"message": fmt.Sprintf("Scan operation timed out after %.0f seconds", config.ScanTimeout.Seconds()),
		})
	}
}

func handleStreamScan(c *gin.Context) {
	// Initialize ClamAV client
	clam, err := getClamdClient()
	if err != nil {
		c.JSON(502, gin.H{
			"status":  "Clamd service down",
			"message": err.Error(),
		})
		return
	}

	// Check content length - reject if missing, -1, or too large
	contentLength := c.Request.ContentLength
	if contentLength <= 0 {
		c.JSON(400, gin.H{
			"message": "Content-Length header is required and must be greater than 0",
		})
		return
	}
	if contentLength > config.MaxContentLength {
		c.JSON(400, gin.H{
			"message": fmt.Sprintf("File too large. Maximum size is %d bytes", config.MaxContentLength),
		})
		return
	}

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
			c.JSON(502, gin.H{
				"status":  "Clamd service down",
				"message": result.Description,
			})
			return
		}

		c.JSON(200, gin.H{
			"status":  result.Status,
			"message": result.Description,
			"time":    elapsed,
		})
	case <-time.After(config.ScanTimeout):
		c.JSON(504, gin.H{
			"status":  "Scan timeout",
			"message": fmt.Sprintf("Scan operation timed out after %.0f seconds", config.ScanTimeout.Seconds()),
		})
	}
}

func handleHealthCheck(c *gin.Context) {
	clam, err := getClamdClient()
	if err != nil {
		c.JSON(502, gin.H{
			"message": "Clamd service unavailable",
		})
		return
	}

	// Ping ClamAV
	err = clam.Ping()
	if err != nil {
		c.JSON(502, gin.H{
			"message": "Clamd service down",
		})
		return
	}

	c.JSON(200, gin.H{
		"message": "ok",
	})
}

func main() {
	// Parse configuration
	parseConfig()

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
		log.Fatalf("Server error: %v", err)
	case sig := <-sigChan:
		log.Printf("Received signal: %v, shutting down...", sig)
	}
}

func startRESTServer(errChan chan<- error) {
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
	log.Printf("Starting REST API server on %s", addr)
	if err := router.Run(addr); err != nil {
		errChan <- fmt.Errorf("REST server error: %w", err)
	}
}

func startGRPCServer(errChan chan<- error) {
	// Create TCP listener
	addr := fmt.Sprintf("%s:%s", config.Host, config.GRPCPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
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

	log.Printf("Starting gRPC server on %s", addr)
	log.Printf("gRPC max message size: %d bytes", maxMsgSize)
	if err := grpcServer.Serve(lis); err != nil {
		errChan <- fmt.Errorf("gRPC server error: %w", err)
	}
}
