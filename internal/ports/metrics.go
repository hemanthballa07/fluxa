package ports

// Metrics abstracts instrumentation so services are not coupled to a specific backend.
// Labels are passed as flat key-value pairs: key, value, key, value, ...
type Metrics interface {
	IncCounter(name string, labels ...string)
	ObserveHistogram(name string, value float64, labels ...string)
}
