package prommetrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var latencyBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5}

// Metrics implements ports.Metrics using Prometheus counters and histograms.
type Metrics struct {
	counters   map[string]*prometheus.CounterVec
	histograms map[string]*prometheus.HistogramVec
}

// NewMetrics creates and registers all Fluxa metrics for the given service.
func NewMetrics(service string) *Metrics {
	counters := map[string]*prometheus.CounterVec{
		"events_ingested_total": prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "events_ingested_total", Help: "Total events accepted by ingest"},
			[]string{"service"},
		),
		"events_processed_total": prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "events_processed_total", Help: "Total events completing the processor pipeline"},
			[]string{"service", "status"},
		),
		"fraud_flags_total": prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "fraud_flags_total", Help: "Total fraud rule fires"},
			[]string{"rule"},
		),
		"query_total": prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "query_total", Help: "Total query endpoint outcomes"},
			[]string{"status"},
		),
		"alerts_consumed_total": prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "alerts_consumed_total", Help: "Total alerts received by alert-consumer"},
			[]string{},
		),
	}

	histograms := map[string]*prometheus.HistogramVec{
		"ingest_latency_seconds": prometheus.NewHistogramVec(
			prometheus.HistogramOpts{Name: "ingest_latency_seconds", Help: "Ingest handler latency", Buckets: latencyBuckets},
			[]string{"service"},
		),
		"process_latency_seconds": prometheus.NewHistogramVec(
			prometheus.HistogramOpts{Name: "process_latency_seconds", Help: "Per-message processor latency", Buckets: latencyBuckets},
			[]string{"service"},
		),
	}

	for _, c := range counters {
		prometheus.MustRegister(c)
	}
	for _, h := range histograms {
		prometheus.MustRegister(h)
	}

	return &Metrics{counters: counters, histograms: histograms}
}

// IncCounter increments the named counter. Labels are flat key-value pairs.
func (m *Metrics) IncCounter(name string, labels ...string) {
	cv, ok := m.counters[name]
	if !ok {
		return
	}
	cv.With(toPromLabels(labels)).Inc()
}

// ObserveHistogram records a value into the named histogram.
func (m *Metrics) ObserveHistogram(name string, value float64, labels ...string) {
	hv, ok := m.histograms[name]
	if !ok {
		return
	}
	hv.With(toPromLabels(labels)).Observe(value)
}

// toPromLabels converts a flat []string of key,value pairs to prometheus.Labels.
func toPromLabels(pairs []string) prometheus.Labels {
	labels := prometheus.Labels{}
	for i := 0; i+1 < len(pairs); i += 2 {
		labels[pairs[i]] = pairs[i+1]
	}
	return labels
}
