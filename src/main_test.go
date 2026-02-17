package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	router.POST("/api/scan", handleScan)
	router.POST("/api/stream-scan", handleStreamScan)
	router.GET("/api/health-check", handleHealthCheck)
	return router
}

func TestHealthCheck(t *testing.T) {
	router := setupRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/health-check", nil)
	router.ServeHTTP(w, req)

	if w.Code == 502 {
		t.Log("Health check returned 502 (ClamAV may not be running)")
		return
	}

	assert.Equal(t, 200, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "ok", response["message"])
}

func TestScanCleanFile(t *testing.T) {
	router := setupRouter()

	// Create a clean test file
	content := []byte("This is a clean file")
	tmpfile, err := os.CreateTemp("", "clean-*.txt")
	assert.NoError(t, err)
	defer os.Remove(tmpfile.Name())
	_, err = tmpfile.Write(content)
	assert.NoError(t, err)
	tmpfile.Close()

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	file, err := os.Open(tmpfile.Name())
	assert.NoError(t, err)
	defer file.Close()

	part, err := writer.CreateFormFile("file", tmpfile.Name())
	assert.NoError(t, err)
	_, err = part.Write(content)
	assert.NoError(t, err)
	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/scan", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	if w.Code == 502 {
		t.Log("Scan returned 502 (ClamAV may not be running)")
		return
	}

	assert.Equal(t, 200, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "OK", response["status"])
}

func TestScanEicarFile(t *testing.T) {
	router := setupRouter()

	// Create EICAR test file
	eicarString := `X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`
	tmpfile, err := os.CreateTemp("", "eicar-*.txt")
	assert.NoError(t, err)
	defer os.Remove(tmpfile.Name())
	_, err = tmpfile.Write([]byte(eicarString))
	assert.NoError(t, err)
	tmpfile.Close()

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	file, err := os.Open(tmpfile.Name())
	assert.NoError(t, err)
	defer file.Close()

	part, err := writer.CreateFormFile("file", tmpfile.Name())
	assert.NoError(t, err)
	_, err = part.Write([]byte(eicarString))
	assert.NoError(t, err)
	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/scan", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	if w.Code == 502 {
		t.Log("Scan returned 502 (ClamAV may not be running)")
		return
	}

	assert.Equal(t, 200, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "FOUND", response["status"])
	// Check if message contains "EICAR" in any case
	assert.True(t, strings.Contains(strings.ToUpper(response["message"].(string)), "EICAR"),
		"Response message should contain 'EICAR': %s", response["message"])
}

func TestStreamScanCleanFile(t *testing.T) {
	router := setupRouter()

	// Create a clean test content
	content := []byte("This is a clean file for streaming test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/stream-scan", bytes.NewReader(content))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(content))
	router.ServeHTTP(w, req)

	if w.Code == 502 {
		t.Log("Stream scan returned 502 (ClamAV may not be running)")
		return
	}

	assert.Equal(t, 200, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "OK", response["status"])
}

func TestStreamScanEicarFile(t *testing.T) {
	router := setupRouter()

	// Create EICAR test content
	eicarString := []byte(`X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/stream-scan", bytes.NewReader(eicarString))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(eicarString))
	router.ServeHTTP(w, req)

	if w.Code == 502 {
		t.Log("Stream scan returned 502 (ClamAV may not be running)")
		return
	}

	assert.Equal(t, 200, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "FOUND", response["status"])
	// Check if message contains "EICAR" in any case
	assert.True(t, strings.Contains(strings.ToUpper(response["message"].(string)), "EICAR"),
		"Response message should contain 'EICAR': %s", response["message"])
}
