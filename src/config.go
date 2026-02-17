package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/dutchcoders/go-clamd"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Build-time variables populated via -ldflags
var (
	Version    = "dev"
	CommitHash = "unknown"
	BuildTime  = "unknown"
)

// Config holds the application configuration
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
		fmt.Fprintf(os.Stderr, "WARNING: invalid value %q for env var %s: %v; using default %v\n", value, key, err, defaultValue)
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
		fmt.Fprintf(os.Stderr, "WARNING: invalid value %q for env var %s: %v; using default %d\n", value, key, err, defaultValue)
	}
	return defaultValue
}

var config = Config{
	Debug:            false,
	ClamdUnixSocket:  getEnvWithDefault("CLAMAV_SOCKET", "/run/clamav/clamd.ctl"),
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

	// Validate configuration values
	if config.ScanTimeout <= 0 {
		fmt.Fprintf(os.Stderr, "FATAL: scan timeout must be > 0, got %v\n", config.ScanTimeout)
		os.Exit(1)
	}
	if config.MaxContentLength <= 0 {
		fmt.Fprintf(os.Stderr, "FATAL: max content length must be > 0, got %d\n", config.MaxContentLength)
		os.Exit(1)
	}
	if config.ClamdUnixSocket == "" {
		fmt.Fprintf(os.Stderr, "FATAL: ClamAV Unix socket path must not be empty\n")
		os.Exit(1)
	}
	if portNum, err := strconv.Atoi(config.Port); err != nil || portNum < 1 || portNum > 65535 {
		fmt.Fprintf(os.Stderr, "FATAL: port must be a valid TCP port (1-65535), got %q\n", config.Port)
		os.Exit(1)
	}
	if grpcPortNum, err := strconv.Atoi(config.GRPCPort); err != nil || grpcPortNum < 1 || grpcPortNum > 65535 {
		fmt.Fprintf(os.Stderr, "FATAL: gRPC port must be a valid TCP port (1-65535), got %q\n", config.GRPCPort)
		os.Exit(1)
	}

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
		zap.String("version", Version),
		zap.String("commit", CommitHash),
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

// clamdClient holds the reusable ClamAV client instance
var (
	clamdClient *clamd.Clamd
	clamdOnce   sync.Once
	clamdMu     sync.Mutex
)

// initClamdClient creates the ClamAV client (call once at startup)
func initClamdClient() {
	clamdClient = clamd.NewClamd("unix://" + config.ClamdUnixSocket)
}

// getClamdClient returns the shared ClamAV client instance.
// It does NOT ping on every call; use pingClamd() for health checks.
// Safe for concurrent use from multiple goroutines.
func getClamdClient() *clamd.Clamd {
	clamdMu.Lock()
	defer clamdMu.Unlock()
	clamdOnce.Do(initClamdClient)
	return clamdClient
}

// resetClamdClient resets the client so the next getClamdClient call
// re-initializes it. Intended for tests that need to swap the socket path.
func resetClamdClient() {
	clamdMu.Lock()
	defer clamdMu.Unlock()
	clamdClient = nil
	clamdOnce = sync.Once{}
}

// pingClamd checks if the ClamAV daemon is reachable
func pingClamd() error {
	return getClamdClient().Ping()
}
