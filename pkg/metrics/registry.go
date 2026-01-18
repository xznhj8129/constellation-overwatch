package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	dto "github.com/prometheus/client_model/go"
)

var (
	// Registry is the global Prometheus registry - initialized once
	Registry *prometheus.Registry
)

func init() {
	Registry = NewRegistry()
}

// NewRegistry creates a new Prometheus registry with Go runtime collectors
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()

	// Go runtime collectors with enhanced runtime metrics
	reg.MustRegister(collectors.NewGoCollector(
		collectors.WithGoCollectorRuntimeMetrics(
			collectors.MetricsGC,
			collectors.MetricsMemory,
			collectors.MetricsScheduler,
		),
	))
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	return reg
}

// MustRegister registers collectors with the global registry, panicking on error
func MustRegister(cs ...prometheus.Collector) {
	Registry.MustRegister(cs...)
}

// Register registers a collector with the global registry
func Register(c prometheus.Collector) error {
	return Registry.Register(c)
}

// Unregister unregisters a collector from the global registry
func Unregister(c prometheus.Collector) bool {
	return Registry.Unregister(c)
}

// Gather gathers all metrics from the global registry
func Gather() ([]*dto.MetricFamily, error) {
	return Registry.Gather()
}
