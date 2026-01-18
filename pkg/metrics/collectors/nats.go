package collectors

import (
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
)

// NATSCollector collects metrics from NATS JetStream
type NATSCollector struct {
	js        nats.JetStreamContext
	msgDesc   *prometheus.Desc
	bytesDesc *prometheus.Desc
	consumers *prometheus.Desc
}

// NewNATSCollector creates a new NATS JetStream collector
func NewNATSCollector(js nats.JetStreamContext) *NATSCollector {
	return &NATSCollector{
		js: js,
		msgDesc: prometheus.NewDesc(
			"overwatch_nats_stream_messages",
			"Number of messages in NATS stream",
			[]string{"stream"}, nil,
		),
		bytesDesc: prometheus.NewDesc(
			"overwatch_nats_stream_bytes",
			"Size of NATS stream in bytes",
			[]string{"stream"}, nil,
		),
		consumers: prometheus.NewDesc(
			"overwatch_nats_stream_consumers",
			"Number of consumers for NATS stream",
			[]string{"stream"}, nil,
		),
	}
}

// Describe implements prometheus.Collector
func (c *NATSCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.msgDesc
	ch <- c.bytesDesc
	ch <- c.consumers
}

// Collect implements prometheus.Collector
func (c *NATSCollector) Collect(ch chan<- prometheus.Metric) {
	if c.js == nil {
		return
	}

	streams := []string{
		"CONSTELLATION_ENTITIES",
		"CONSTELLATION_EVENTS",
		"CONSTELLATION_TELEMETRY",
		"CONSTELLATION_COMMANDS",
		"CONSTELLATION_VIDEO_FRAMES",
	}

	for _, name := range streams {
		info, err := c.js.StreamInfo(name)
		if err != nil {
			continue
		}

		ch <- prometheus.MustNewConstMetric(
			c.msgDesc, prometheus.GaugeValue,
			float64(info.State.Msgs), name,
		)
		ch <- prometheus.MustNewConstMetric(
			c.bytesDesc, prometheus.GaugeValue,
			float64(info.State.Bytes), name,
		)
		ch <- prometheus.MustNewConstMetric(
			c.consumers, prometheus.GaugeValue,
			float64(info.State.Consumers), name,
		)
	}
}
