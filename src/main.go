package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/dutchcoders/go-clamd"
	"github.com/gin-gonic/gin"
)

type Config struct {
	Debug            bool
	ClamdUnixSocket  string
	MaxContentLength int64
	Host             string
	Port             string
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
}

func parseConfig() {
	// Command line flags
	debug := flag.Bool("debug", config.Debug, "Enable debug mode")
	socket := flag.String("socket", config.ClamdUnixSocket, "ClamAV Unix socket path")
	maxSize := flag.Int64("max-size", config.MaxContentLength, "Maximum file size in bytes")
	host := flag.String("host", config.Host, "Host to listen on")
	port := flag.String("port", config.Port, "Port to listen on")

	// Parse flags
	flag.Parse()

	// Update config with environment variables or flags
	config.Debug = getEnvBoolWithDefault("CLAMAV_DEBUG", *debug)
	config.ClamdUnixSocket = getEnvWithDefault("CLAMAV_SOCKET", *socket)
	config.MaxContentLength = getEnvInt64WithDefault("CLAMAV_MAX_SIZE", *maxSize)
	config.Host = getEnvWithDefault("CLAMAV_HOST", *host)
	config.Port = getEnvWithDefault("CLAMAV_PORT", *port)

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
	log.Printf("Listen Address: %s:%s", config.Host, config.Port)
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
	response, scanErr := clam.ScanStream(file, done)
	if scanErr != nil {
		c.JSON(502, gin.H{
			"status":  "Clamd service down",
			"message": scanErr.Error(),
		})
		return
	}

	// Process scan results
	result := <-response // Get first result from channel
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

	// Initialize router
	router := gin.Default()

	// Set maximum multipart memory
	router.MaxMultipartMemory = config.MaxContentLength

	// Register routes
	router.POST("/api/scan", handleScan)
	router.GET("/api/health-check", handleHealthCheck)

	// Start server
	addr := fmt.Sprintf("%s:%s", config.Host, config.Port)
	log.Printf("Starting server on %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatal(err)
	}
}
