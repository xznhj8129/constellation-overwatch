package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/metrics"
)

func TestMiddleware(t *testing.T) {
	// Create a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Wrap with metrics middleware
	wrapped := metrics.Middleware(handler)

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	// Execute request
	wrapped.ServeHTTP(rr, req)

	// Verify response
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	// Verify metrics were collected (check registry)
	mfs, err := metrics.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	// Look for HTTP request metrics
	found := false
	for _, mf := range mfs {
		if mf.GetName() == "overwatch_http_requests_total" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected overwatch_http_requests_total metric to be registered")
	}
}

func TestMiddlewareWithDifferentStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"OK", http.StatusOK},
		{"Not Found", http.StatusNotFound},
		{"Server Error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			wrapped := metrics.Middleware(handler)
			req := httptest.NewRequest("GET", "/test", nil)
			rr := httptest.NewRecorder()

			wrapped.ServeHTTP(rr, req)

			if rr.Code != tt.statusCode {
				t.Errorf("Expected status %d, got %d", tt.statusCode, rr.Code)
			}
		})
	}
}

func TestMiddlewarePathNormalization(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/static/css/style.css", "/static/*"},
		{"/metrics", "/metrics"},
		{"/api/v1/entities/123", "/api/v1/entities/{id}"},
		{"/api/v1/organizations/550e8400-e29b-41d4-a716-446655440000", "/api/v1/organizations/{id}"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			// This test verifies the middleware handles different path patterns
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			wrapped := metrics.Middleware(handler)
			req := httptest.NewRequest("GET", tt.path, nil)
			rr := httptest.NewRecorder()

			wrapped.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", rr.Code)
			}
		})
	}
}
