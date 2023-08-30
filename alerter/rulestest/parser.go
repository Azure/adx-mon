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

type TestInput struct {
	Name           string
	Values         []string
	Interval       time.Duration
	ExpectedAlerts []*alert.Alert
}

type Test struct {
	name           string
	timeseries     map[string][]Timeseries
	interval       time.Duration
	expectedAlerts []*alert.Alert
}

type Timeseries struct {
	metric string
	labels map[string]string
	values []parser.SequenceValue
}

func (t *Timeseries) seriesID() uint64 {
	var buf strings.Builder
	for k, v := range t.labels {
		buf.WriteString(fmt.Sprintf("%s:%s,", k, v))
	}
	return xxhash.Sum64String(buf.String())
}

func (t *Timeseries) stringFor(startTime time.Time, interval time.Duration, liftedLabels []string) (string, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\tdatetime(%s),", startTime.Format(time.RFC3339)))

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
		sb.WriteString(fmt.Sprintf("\tdatetime(%s), %d, dynamic(%s), %f,", now.Format(time.RFC3339), seriesID, string(o), value.Value))

		for _, key := range liftedLabels {
			sb.WriteString(fmt.Sprintf("%s,", liftedMap[key]))
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

func Parse(raw string) (*Test, error) {
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

	if err := test.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate test: %w", err)
	}
	return test, nil
}

func (t *Test) Validate() error {
	if t.name == "" {
		return fmt.Errorf("missing name")
	}
	if t.interval == 0 {
		return fmt.Errorf("missing interval")
	}
	if len(t.timeseries) == 0 {
		return fmt.Errorf("missing values")
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
	return nil
}

// datatable(Date:datetime, Event:string, MoreData:dynamic) [
//     datetime(1910-06-11), "Born", dynamic({"key1":"value1", "key2":"value2"}),
//     datetime(1930-01-01), "Enters Ecole Navale", dynamic({"key1":"value3", "key2":"value4"}),
//     datetime(1953-01-01), "Published first book", dynamic({"key1":"value5", "key2":"value6"}),
//     datetime(1997-06-25), "Died", dynamic({"key1":"value7", "key2":"value8"}),
// ]

func (t *Test) GetDatatableStmt(liftedLabels []string) (string, error) {
	var sb strings.Builder
	startTime := time.Now()
	// input: `CPUContainerSpecShares{Underlay: "cx-test1", Overlay: "cx-test2", ClusterID: "0000000000"} 0x15 1+1x20`,
	schemaStr := getSchemaStr(liftedLabels)
	for metric, ts := range t.timeseries {
		sb.WriteString(fmt.Sprintf("let %s datatable(%s) [\n", metric, schemaStr))
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
	for _, mapping := range defaultMapping {
		sb.WriteString(fmt.Sprintf("%s:%s,", mapping.Column, mapping.DataType))
	}
	for _, label := range extraStringLabels {
		sb.WriteString(fmt.Sprintf("%s:string,", label))
	}
	// TODO: don't write comma out if no extra labels
	return sb.String()
}
