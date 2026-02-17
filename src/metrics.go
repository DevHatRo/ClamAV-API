package main

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	scanRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "clamav_scan_requests_total",
			Help: "Total number of scan requests by method and result status",
		},
		[]string{"method", "status"},
	)

	scanDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "clamav_scan_duration_seconds",
			Help:    "Duration of scan operations in seconds",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
		},
		[]string{"method"},
	)

	scansInProgress = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "clamav_scans_in_progress",
			Help: "Number of scans currently in progress",
		},
	)

	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "clamav_http_requests_total",
			Help: "Total number of HTTP requests by path and status code",
		},
		[]string{"method", "path", "status_code"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "clamav_http_request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	healthCheckStatus = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "clamav_health_check_healthy",
			Help: "Whether ClamAV is healthy (1) or unhealthy (0)",
		},
	)
)

// metricsMiddleware records HTTP request metrics for all endpoints
func metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		elapsed := time.Since(start).Seconds()
		statusCode := strconv.Itoa(c.Writer.Status())

		httpRequestsTotal.WithLabelValues(c.Request.Method, c.FullPath(), statusCode).Inc()
		httpRequestDuration.WithLabelValues(c.Request.Method, c.FullPath()).Observe(elapsed)
	}
}

// recordScanMetrics records scan-specific metrics (duration and request count)
func recordScanMetrics(method string, result *ScanResult, err error) {
	status := "ok"
	if err != nil {
		switch err.(type) {
		case *ScanTimeoutError:
			status = "timeout"
		case *ScanEngineError:
			status = "engine_error"
		default:
			status = "error"
		}
	} else if result != nil && result.Status == "FOUND" {
		status = "found"
	}

	scanRequestsTotal.WithLabelValues(method, status).Inc()

	if result != nil {
		scanDurationSeconds.WithLabelValues(method).Observe(result.ScanTime)
	}
}
