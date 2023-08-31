package rulestest

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/cespare/xxhash"
	"github.com/prometheus/prometheus/promql/parser"
	"golang.org/x/exp/slices"
	"gopkg.in/yaml.v2"
)

var (
	defaultMapping = storage.NewMetricsSchema()
)

type ExpectedAlert struct {
	Name   string
	EvalAt time.Duration `yaml:"eval_at"`
	Alert  *alert.Alert
}

type TestInput struct {
	Name           string
	Values         []string
	Interval       time.Duration
	ExpectedAlerts []ExpectedAlert `yaml:"expected_alerts"`
}

type Test struct {
	name           string
	timeseries     map[string][]Timeseries
	interval       time.Duration
	expectedAlerts []ExpectedAlert
}

type Timeseries struct {
	metric string
	labels map[string]string
	values []parser.SequenceValue
}

func (t *Timeseries) seriesID() uint64 {
	var buf strings.Builder
	sortedLabels := make([]string, 0, len(t.labels))

	for k, v := range t.labels {
		sortedLabels = append(sortedLabels, fmt.Sprintf("%s=%s", k, v))
	}

	slices.Sort(sortedLabels)
	for _, v := range sortedLabels {
		buf.WriteString(fmt.Sprintf("%v,", v))
	}
	return xxhash.Sum64String(buf.String())
}

func (t *Timeseries) stringFor(startTime time.Time, interval time.Duration, liftedLabels []string) (string, error) {
	var sb strings.Builder
	seriesID := t.seriesID()
	liftedMap, labelMap := t.separateLabels(liftedLabels)

	o, err := json.Marshal(labelMap)
	if err != nil {
		return "", err
	}

	for i, value := range t.values {
		if value.Omitted {
			continue
		}
		now := startTime.Add(interval * time.Duration(i))
		sb.WriteString(fmt.Sprintf("\tdatetime(%s), %#x, dynamic(%s), %f,", now.Format(time.RFC3339), seriesID, string(o), value.Value))

		for _, key := range liftedLabels {
			sb.WriteString(fmt.Sprintf("\"%s\",", liftedMap[key]))
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

func (t *Timeseries) separateLabels(liftedLabels []string) (map[string]string, map[string]string) {
	liftedMap := make(map[string]string)
	labelMap := make(map[string]string)
	for k, v := range t.labels {
		if slices.Contains(liftedLabels, k) {
			liftedMap[k] = v
		} else {
			labelMap[k] = v
		}
	}
	return liftedMap, labelMap
}

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

func (t *Test) validate() error {
	if t.name == "" {
		return fmt.Errorf("missing name")
	}
	if t.interval == 0 {
		return fmt.Errorf("missing interval")
	}
	if len(t.timeseries) == 0 {
		return fmt.Errorf("missing values")
	}
	if len(t.expectedAlerts) == 0 {
		return fmt.Errorf("missing expected alerts")
	}
	for metric, ts := range t.timeseries {
		if len(ts) == 0 {
			return fmt.Errorf("missing timeseries for metric %s", metric)
		}
		for i, value := range ts {
			if value.metric != metric {
				return fmt.Errorf("incorrectly constructed metric name %s does not match timeseries %s", metric, value.metric)
			}
			if value.metric == "" {
				return fmt.Errorf("missing metric on timeseries %d", i)
			}
		}
	}
	for i, expectedAlert := range t.expectedAlerts {
		if expectedAlert.Name == "" {
			return fmt.Errorf("missing name on expected alert %d", i)
		}
		if expectedAlert.EvalAt == 0 {
			return fmt.Errorf("evalAt is either missing or 0 on expected alert %d. evalAt must be positive. It is the time after the start of the input series at which the alertrule will be evaluated", i)
		}
	}
	return nil
}

func (t *Test) getDatatableStmt(liftedLabels []string) (string, error) {
	var sb strings.Builder
	startTime := time.Time{}
	schemaStr := getSchemaStr(liftedLabels)
	for metric, ts := range t.timeseries {
		sb.WriteString(fmt.Sprintf("let %s = datatable(%s) [\n", metric, schemaStr))
		for _, v := range ts {
			tsOutput, err := v.stringFor(startTime, t.interval, liftedLabels)
			if err != nil {
				return "", err
			}
			sb.WriteString(tsOutput)
		}
		sb.WriteString("];\n")
	}
	return sb.String(), nil
}

func getSchemaStr(extraStringLabels []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s:%s", defaultMapping[0].Column, defaultMapping[0].DataType))

	for _, mapping := range defaultMapping[1:] {
		sb.WriteString(fmt.Sprintf(", %s:%s", mapping.Column, mapping.DataType))
	}
	for _, label := range extraStringLabels {
		sb.WriteString(fmt.Sprintf(", %s:string", label))
	}
	return sb.String()
}
