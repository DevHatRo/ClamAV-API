package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
)

func getCounterValue(counter *prometheus.CounterVec, labels ...string) float64 {
	m := &io_prometheus_client.Metric{}
	c, err := counter.GetMetricWithLabelValues(labels...)
	if err != nil {
		return 0
	}
	_ = c.(prometheus.Metric).Write(m)
	return m.GetCounter().GetValue()
}

func TestMetricsMiddlewareSkipsPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(metricsMiddleware())
	router.GET("/metrics", func(c *gin.Context) { c.String(200, "metrics") })
	router.GET("/api/health-check", func(c *gin.Context) { c.String(200, "ok") })

	// Record baseline
	baseMetrics := getCounterValue(httpRequestsTotal, "GET", "/metrics", "200")
	baseHealth := getCounterValue(httpRequestsTotal, "GET", "/api/health-check", "200")

	// Hit /metrics
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/metrics", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)

	// Hit /api/health-check
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/health-check", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)

	// Counters should NOT have increased for skipped paths
	assert.Equal(t, baseMetrics, getCounterValue(httpRequestsTotal, "GET", "/metrics", "200"))
	assert.Equal(t, baseHealth, getCounterValue(httpRequestsTotal, "GET", "/api/health-check", "200"))
}

func TestMetricsMiddlewareRecordsPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(metricsMiddleware())
	router.POST("/api/scan", func(c *gin.Context) { c.String(200, "scanned") })

	// Record baseline
	baseScan := getCounterValue(httpRequestsTotal, "POST", "/api/scan", "200")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/scan", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)

	// Counter should have increased
	assert.Equal(t, baseScan+1, getCounterValue(httpRequestsTotal, "POST", "/api/scan", "200"))
}

func TestRecordScanMetricsSuccess(t *testing.T) {
	base := getCounterValue(scanRequestsTotal, "test_method", "ok")
	result := &ScanResult{Status: "OK", Description: "", ScanTime: 0.5}
	recordScanMetrics("test_method", result, nil)
	assert.Equal(t, base+1, getCounterValue(scanRequestsTotal, "test_method", "ok"))
}

func TestRecordScanMetricsFound(t *testing.T) {
	base := getCounterValue(scanRequestsTotal, "test_found", "found")
	result := &ScanResult{Status: "FOUND", Description: "Eicar-Test", ScanTime: 0.3}
	recordScanMetrics("test_found", result, nil)
	assert.Equal(t, base+1, getCounterValue(scanRequestsTotal, "test_found", "found"))
}

func TestRecordScanMetricsTimeout(t *testing.T) {
	base := getCounterValue(scanRequestsTotal, "test_timeout", "timeout")
	recordScanMetrics("test_timeout", nil, &ScanTimeoutError{Timeout: 30 * time.Second})
	assert.Equal(t, base+1, getCounterValue(scanRequestsTotal, "test_timeout", "timeout"))
}

func TestRecordScanMetricsEngineError(t *testing.T) {
	base := getCounterValue(scanRequestsTotal, "test_engine", "engine_error")
	recordScanMetrics("test_engine", nil, &ScanEngineError{Description: "broken", ScanTime: 0.5})
	assert.Equal(t, base+1, getCounterValue(scanRequestsTotal, "test_engine", "engine_error"))
}

func TestRecordScanMetricsGenericError(t *testing.T) {
	base := getCounterValue(scanRequestsTotal, "test_generic", "error")
	recordScanMetrics("test_generic", nil, errors.New("generic"))
	assert.Equal(t, base+1, getCounterValue(scanRequestsTotal, "test_generic", "error"))
}

func TestRecordScanMetricsEngineErrorWithScanTime(t *testing.T) {
	// This tests the branch where engineErr is non-nil and has ScanTime > 0
	engineErr := &ScanEngineError{Description: "err", ScanTime: 2.5}
	recordScanMetrics("test_engine_time", nil, engineErr)
	// Just verify it doesn't panic - the histogram is recorded
}
