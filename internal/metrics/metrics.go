package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Metrics handles CloudWatch Embedded Metric Format (EMF) logging
type Metrics struct {
	namespace         string
	defaultDimensions map[string]string
}

// NewMetrics creates a new metrics logger with service and env dimensions
func NewMetrics(namespace, service string) *Metrics {
	env := os.Getenv("CONFIG_ENV")
	if env == "" {
		env = "dev"
	}
	return &Metrics{
		namespace: namespace,
		defaultDimensions: map[string]string{
			"Service":     service,
			"Environment": env,
		},
	}
}

// EmitMetric emits a CloudWatch metric using EMF
func (m *Metrics) EmitMetric(metricName string, value float64, unit string, dimensions map[string]string) error {
	// Merge default dimensions with provided dimensions
	allDimensions := make(map[string]string)
	for k, v := range m.defaultDimensions {
		allDimensions[k] = v
	}
	for k, v := range dimensions {
		allDimensions[k] = v
	}

	// Convert to proper EMF format
	emfData := map[string]interface{}{
		"_aws": map[string]interface{}{
			"CloudWatchMetrics": []map[string]interface{}{
				{
					"Namespace":  m.namespace,
					"Dimensions": buildDimensions(allDimensions),
					"Metrics": []map[string]interface{}{
						{
							"MetricName": metricName,
							"Unit":       unit,
						},
					},
				},
			},
			"Timestamp": getTimestamp(),
		},
		metricName: value,
	}

	// Add dimensions as fields
	for k, v := range allDimensions {
		emfData[k] = v
	}

	bytes, err := json.Marshal(emfData)
	if err != nil {
		return fmt.Errorf("failed to marshal EMF: %w", err)
	}

	os.Stdout.Write(append(bytes, '\n'))
	return nil
}

func buildDimensions(dims map[string]string) [][]string {
	if len(dims) == 0 {
		return [][]string{}
	}

	keys := make([]string, 0, len(dims))
	for k := range dims {
		keys = append(keys, k)
	}

	// EMF Dimensions is a list of lists of dimension names
	return [][]string{keys}
}

func getTimestamp() int64 {
	return time.Now().UnixMilli()
}
