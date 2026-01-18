package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns the Prometheus metrics handler for the global registry
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
		Registry:          Registry,
	})
}

// HandlerFunc returns the Prometheus metrics handler as an http.HandlerFunc
func HandlerFunc() http.HandlerFunc {
	h := Handler()
	return h.ServeHTTP
}
