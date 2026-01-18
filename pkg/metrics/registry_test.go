package metrics_test

import (
	"testing"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/metrics"
)

func TestNewRegistry(t *testing.T) {
	reg := metrics.NewRegistry()
	if reg == nil {
		t.Fatal("NewRegistry returned nil")
	}

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	// Verify Go runtime metrics are present
	found := make(map[string]bool)
	for _, mf := range mfs {
		found[mf.GetName()] = true
	}

	required := []string{"go_goroutines", "go_memstats_alloc_bytes"}
	for _, name := range required {
		if !found[name] {
			t.Errorf("Expected metric %q not found", name)
		}
	}
}

func TestGlobalRegistry(t *testing.T) {
	if metrics.Registry == nil {
		t.Fatal("Global Registry is nil")
	}

	mfs, err := metrics.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics from global registry: %v", err)
	}

	if len(mfs) == 0 {
		t.Error("Expected some metrics from global registry")
	}
}

func TestHandler(t *testing.T) {
	handler := metrics.Handler()
	if handler == nil {
		t.Fatal("Handler returned nil")
	}
}

func TestHandlerFunc(t *testing.T) {
	handlerFunc := metrics.HandlerFunc()
	if handlerFunc == nil {
		t.Fatal("HandlerFunc returned nil")
	}
}
