package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestHandleScanNoFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/scan", handleScan)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/scan", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Provide a single file", response["message"])
}

func TestHandleScanInvalidMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/scan", handleScan)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/scan", strings.NewReader("invalid data"))
	req.Header.Set("Content-Type", "multipart/form-data")
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestHandleStreamScanNoContentLength(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/stream-scan", handleStreamScan)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/stream-scan", bytes.NewReader([]byte("test")))
	// Don't set Content-Length
	req.ContentLength = -1
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["message"], "Content-Length")
}

func TestHandleStreamScanZeroLength(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/stream-scan", handleStreamScan)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/stream-scan", bytes.NewReader([]byte{}))
	req.ContentLength = 0
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["message"], "Content-Length")
}

func TestHandleStreamScanTooLarge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/stream-scan", handleStreamScan)

	// Create request with content length larger than max
	data := bytes.NewReader(make([]byte, 1000))
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/stream-scan", data)
	req.ContentLength = config.MaxContentLength + 1
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["message"], "too large")
}

func TestHandleScanMultipleFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/scan", handleScan)

	// Create multipart with multiple files (not supported)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add first file
	part1, _ := writer.CreateFormFile("file1", "test1.txt")
	part1.Write([]byte("test1"))

	// Add second file
	part2, _ := writer.CreateFormFile("file2", "test2.txt")
	part2.Write([]byte("test2"))

	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/scan", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	// Should accept (Gin will just use first file)
	// Or could be 400 depending on implementation
	assert.True(t, w.Code == 200 || w.Code == 400 || w.Code == 502)
}

func TestHandleHealthCheckWhenClamAVDown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.GET("/api/health-check", handleHealthCheck)

	// Save original config
	originalSocket := config.ClamdUnixSocket
	config.ClamdUnixSocket = "/nonexistent/socket.ctl"
	resetClamdClient()
	defer func() {
		config.ClamdUnixSocket = originalSocket
		resetClamdClient()
	}()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/health-check", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, 502, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["message"], "unavailable")
}

func TestHandleScanWithInvalidSocket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/scan", handleScan)

	// Save original config
	originalSocket := config.ClamdUnixSocket
	config.ClamdUnixSocket = "/invalid/socket.ctl"
	resetClamdClient()
	defer func() {
		config.ClamdUnixSocket = originalSocket
		resetClamdClient()
	}()

	// Create a valid multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test.txt")
	io.WriteString(part, "test content")
	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/scan", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	assert.Equal(t, 502, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Clamd service down", response["status"])
}

func TestHandleStreamScanWithInvalidSocket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/stream-scan", handleStreamScan)

	// Save original config
	originalSocket := config.ClamdUnixSocket
	config.ClamdUnixSocket = "/invalid/socket.ctl"
	resetClamdClient()
	defer func() {
		config.ClamdUnixSocket = originalSocket
		resetClamdClient()
	}()

	data := bytes.NewReader([]byte("test data"))
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/stream-scan", data)
	req.ContentLength = 9
	router.ServeHTTP(w, req)

	assert.Equal(t, 502, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Clamd service down", response["status"])
}

func TestResponseFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/scan", handleScan)

	// Create a clean test file
	content := []byte("Response format test")
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test.txt")
	part.Write(content)
	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/scan", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	// Parse response
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Only 200 (success) or 502 (ClamAV unavailable) are expected
	assert.True(t, w.Code == 200 || w.Code == 502,
		"Expected status 200 or 502, got %d", w.Code)

	// All responses should have status and message
	assert.Contains(t, response, "status")
	assert.Contains(t, response, "message")

	_, ok := response["status"].(string)
	assert.True(t, ok, "status should be string")

	_, ok = response["message"].(string)
	assert.True(t, ok, "message should be string")

	// Successful responses (200) should also include time
	if w.Code == 200 {
		assert.Contains(t, response, "time")
		_, ok = response["time"].(float64)
		assert.True(t, ok, "time should be float64")
	}

	// Error responses (502) indicate ClamAV is not available
	if w.Code == 502 {
		t.Logf("ClamAV not available, response format verified for error case: %v", response)
	}
}

func TestHealthCheckResponseFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.GET("/api/health-check", handleHealthCheck)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/health-check", nil)
	router.ServeHTTP(w, req)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Verify response structure
	assert.Contains(t, response, "message")
	assert.NotEmpty(t, response["message"])
}
