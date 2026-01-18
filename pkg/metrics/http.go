package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	httpRequestsTotal *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	httpRequestsInFlight prometheus.Gauge
	httpMetricsOnce sync.Once
)

// initHTTPMetrics initializes HTTP metrics collectors
func initHTTPMetrics() {
	httpMetricsOnce.Do(func() {
		httpRequestsTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "overwatch",
				Name:      "http_requests_total",
				Help:      "Total HTTP requests",
			},
			[]string{"method", "path", "status"},
		)

		httpRequestDuration = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "overwatch",
				Name:      "http_request_duration_seconds",
				Help:      "HTTP request duration in seconds",
				Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
			},
			[]string{"method", "path"},
		)

		httpRequestsInFlight = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "overwatch",
				Name:      "http_requests_in_flight",
				Help:      "Number of HTTP requests currently being processed",
			},
		)

		Registry.MustRegister(httpRequestsTotal, httpRequestDuration, httpRequestsInFlight)
	})
}

// Middleware wraps an http.Handler with Prometheus metrics collection
func Middleware(next http.Handler) http.Handler {
	initHTTPMetrics()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		httpRequestsInFlight.Inc()
		defer httpRequestsInFlight.Dec()

		wrapped := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start).Seconds()
		path := normalizePath(r.URL.Path)

		httpRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(wrapped.status)).Inc()
		httpRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
	})
}

// MiddlewareFunc returns the middleware as a function for use with chi router
func MiddlewareFunc(next http.Handler) http.Handler {
	return Middleware(next)
}

// statusRecorder wraps http.ResponseWriter to capture the status code
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(b)
}

// Flush implements http.Flusher for SSE support
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// normalizePath normalizes URL paths to reduce cardinality
// e.g., /api/v1/entities/123 -> /api/v1/entities/{id}
func normalizePath(path string) string {
	// Skip normalization for static files
	if strings.HasPrefix(path, "/static/") {
		return "/static/*"
	}

	// Skip normalization for metrics endpoint
	if path == "/metrics" {
		return path
	}

	// Normalize paths with IDs
	parts := strings.Split(path, "/")
	for i, part := range parts {
		// If this part looks like a UUID or numeric ID, replace it
		if isID(part) {
			parts[i] = "{id}"
		}
	}

	return strings.Join(parts, "/")
}

// isID checks if a string looks like an ID (UUID or numeric)
func isID(s string) bool {
	if s == "" {
		return false
	}

	// Check if numeric
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}

	// Check if UUID-like (36 chars with hyphens)
	if len(s) == 36 && strings.Count(s, "-") == 4 {
		return true
	}

	// Check if UUID without hyphens (32 hex chars)
	if len(s) == 32 {
		for _, c := range s {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
		return true
	}

	return false
}
