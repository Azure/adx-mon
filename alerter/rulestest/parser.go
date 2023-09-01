package rulestest

import (
	"fmt"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/prometheus/prometheus/promql/parser"
	"gopkg.in/yaml.v2"
)

var (
	defaultMapping = storage.NewMetricsSchema()
)

// ExpectedAlert is the expected result for executing the rule with the given `Name` at `EvalAt`.
type ExpectedAlerts struct {
	Name   string
	EvalAt time.Duration `yaml:"eval_at"`
	Alerts []*alert.Alert
}

type TestInput struct {
	Name           string
	Values         []string
	Interval       time.Duration
	ExpectedAlerts []ExpectedAlerts `yaml:"expected_alerts"`
}

// parse parses a TestInput from a yaml string and returns the corresponding Test struct, or error.
func parse(raw string) (*Test, error) {
	testInput := TestInput{}
	err := yaml.Unmarshal([]byte(raw), &testInput)
	if err != nil {
		return nil, fmt.Errorf("failed to parse input: %w", err)
	}

	test := &Test{
		timeseries: make(map[string][]Timeseries),
	}
	test.name = testInput.Name
	test.interval = testInput.Interval
	test.expectedAlerts = testInput.ExpectedAlerts
	for i, value := range testInput.Values {
		labels, vals, err := parser.ParseSeriesDesc(value)
		if err != nil {
			return nil, fmt.Errorf("failed to parse series values at index %d: %s on test %s on : %w", i, value, test.name, err)
		}

		t := Timeseries{
			labels: make(map[string]string),
			values: vals,
		}
		for _, label := range labels {
			if label.Name == "__name__" {
				t.metric = label.Value
			} else {
				t.labels[label.Name] = label.Value
			}
		}
		test.timeseries[t.metric] = append(test.timeseries[t.metric], t)
	}

	if err := test.validate(); err != nil {
		return nil, fmt.Errorf("failed to validate test: %w", err)
	}
	return test, nil
}
