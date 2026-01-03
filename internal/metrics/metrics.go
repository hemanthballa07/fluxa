package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Metrics handles CloudWatch Embedded Metric Format (EMF) logging
type Metrics struct {
	namespace string
}

// NewMetrics creates a new metrics logger
func NewMetrics(namespace string) *Metrics {
	return &Metrics{
		namespace: namespace,
	}
}

// EmitMetric emits a CloudWatch metric using EMF
func (m *Metrics) EmitMetric(metricName string, value float64, unit string, dimensions map[string]string) error {
	// Convert to proper EMF format
	emfData := map[string]interface{}{
		"_aws": map[string]interface{}{
			"CloudWatchMetrics": []map[string]interface{}{
				{
					"Namespace":  m.namespace,
					"Dimensions": buildDimensions(dimensions),
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
	for k, v := range dimensions {
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
	values := make([]string, 0, len(dims))
	for k, v := range dims {
		keys = append(keys, k)
		values = append(values, v)
	}

	return [][]string{keys, values}
}

func getTimestamp() int64 {
	return time.Now().UnixMilli()
}
