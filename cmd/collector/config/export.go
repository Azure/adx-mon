package config

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/Azure/adx-mon/collector/export"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/Azure/adx-mon/transform"
)

type Exporters struct {
	OtlpMetricExport       []*OtlpMetricExport       `toml:"otlp-metric-export" comment:"Configuration for exporting metrics to an OTLP/HTTP endpoint."`
	FluentForwardLogExport []*FluentForwardLogExport `toml:"fluent-forward-log-export" comment:"Configuration for exporting logs to a Fluentd/Fluent Bit endpoint."`
}

func (e *Exporters) Validate() error {
	exporterNames := make(map[string]struct{})
	for i, exporter := range e.OtlpMetricExport {
		if err := exporter.Validate(); err != nil {
			return fmt.Errorf("exporter.otlp-metric-export[%d].%w", i, err)
		}
		if _, ok := exporterNames[exporter.Name]; ok {
			return fmt.Errorf("exporter.otlp-metric-export[%d].name %q is not unique", i, exporter.Name)
		}
		exporterNames[exporter.Name] = struct{}{}
	}
	for i, exporter := range e.FluentForwardLogExport {
		if err := exporter.Validate(); err != nil {
			return fmt.Errorf("exporter.fluent-forward-log-export[%d].%w", i, err)
		}
		if _, ok := exporterNames[exporter.Name]; ok {
			return fmt.Errorf("exporter.fluent-forward-log-export[%d].name %q is not unique", i, exporter.Name)
		}
		exporterNames[exporter.Name] = struct{}{}
	}
	return nil
}

func GetMetricsExporter(name string, e *Exporters) (remote.RemoteWriteClient, error) {
	if e == nil {
		return nil, fmt.Errorf("exporters config not set")
	}
	for _, exporter := range e.OtlpMetricExport {
		if exporter.Name == name {
			return exporter.GetWriteClient()
		}
	}
	return nil, fmt.Errorf("exporter %s not found", name)
}

func HasMetricsExporter(name string, e *Exporters) bool {
	if e == nil {
		return false
	}
	for _, exporter := range e.OtlpMetricExport {
		if exporter.Name == name {
			return true
		}
	}
	return false
}

func GetLogsExporter(name string, e *Exporters) (types.Sink, error) {
	if e == nil {
		return nil, fmt.Errorf("exporters config not set")
	}
	for _, exporter := range e.FluentForwardLogExport {
		if exporter.Name == name {
			opts := export.LogToFluentExporterOpts{
				Destination:  exporter.Destination,
				TagAttribute: exporter.TagAttribute,
			}
			return export.NewLogToFluentExporter(opts)
		}
	}
	return nil, fmt.Errorf("exporter %s not found", name)
}

func HasLogsExporter(name string, e *Exporters) bool {
	if e == nil {
		return false
	}
	for _, exporter := range e.FluentForwardLogExport {
		if exporter.Name == name {
			return true
		}
	}
	return false
}

// OtlpMetricExport exports metric telemetry to OTLP/HTTP metrics endpoints.
type OtlpMetricExport struct {
	Name        string `toml:"name" comment:"Name of the exporter."`
	Destination string `toml:"destination" comment:"OTLP/HTTP endpoint to send metrics to."`

	DefaultDropMetrics        *bool             `toml:"default-drop-metrics" comment:"Default to dropping all metrics.  Only metrics matching a keep rule will be kept."`
	AddLabels                 map[string]string `toml:"add-labels,omitempty" comment:"Key/value pairs of labels to add to all metrics."`
	DropLabels                map[string]string `toml:"drop-labels" comment:"Labels to drop if they match a metrics regex in the format <metrics regex>=<label name>."`
	DropMetrics               []string          `toml:"drop-metrics" comment:"Regexes of metrics to drop."`
	KeepMetrics               []string          `toml:"keep-metrics" comment:"Regexes of metrics to keep."`
	KeepMetricsWithLabelValue []LabelMatcher    `toml:"keep-metrics-with-label-value" comment:"Regexes of metrics to keep if they have the given label and value."`
	AddResourceAttributes     map[string]string `toml:"add-resource-attributes" comment:"Key/value pairs of resource attributes to add to all metrics."`
}

func (o *OtlpMetricExport) Validate() error {
	if o.Name == "" {
		return fmt.Errorf("name must be set")
	}
	if o.Destination == "" {
		return fmt.Errorf("destination must be set")
	}
	_, err := o.GetWriteClient()
	if err != nil {
		return fmt.Errorf("invalid exporter config: %w", err)
	}
	return nil
}

func (o *OtlpMetricExport) GetWriteClient() (remote.RemoteWriteClient, error) {
	transformer, err := NewTransformer(o.DefaultDropMetrics, o.AddLabels, o.DropLabels, o.DropMetrics, o.KeepMetrics, o.KeepMetricsWithLabelValue)
	if err != nil {
		return nil, err
	}

	opts := export.PromToOtlpExporterOpts{
		Transformer:           transformer,
		Destination:           o.Destination,
		AddResourceAttributes: o.AddResourceAttributes,
	}
	return export.NewPromToOtlpExporter(opts), nil
}

func NewTransformer(defaultDropMetrics *bool, addLabels map[string]string, dropLabels map[string]string, dropMetrics, keepMetrics []string, keepMetricsWithLabelValue []LabelMatcher) (*transform.RequestTransformer, error) {
	dropLabelsMap, err := getRegexMappings(dropLabels)
	if err != nil {
		return nil, err
	}

	dropMetricsRegex, err := getRegexList(dropMetrics)
	if err != nil {
		return nil, err
	}

	keepMetricsRegex, err := getRegexList(keepMetrics)
	if err != nil {
		return nil, err
	}

	keepMetricsWithLabelValueMap, err := getLabelMappings(keepMetricsWithLabelValue)
	if err != nil {
		return nil, err
	}

	return &transform.RequestTransformer{
		DefaultDropMetrics:        getDefaultDropMetrics(defaultDropMetrics),
		AddLabels:                 addLabels,
		DropLabels:                dropLabelsMap,
		DropMetrics:               dropMetricsRegex,
		KeepMetrics:               keepMetricsRegex,
		KeepMetricsWithLabelValue: keepMetricsWithLabelValueMap,
	}, nil
}

func getDefaultDropMetrics(defaultDropMetrics *bool) bool {
	if defaultDropMetrics == nil {
		return false
	}
	return *defaultDropMetrics
}

func getRegexList(regexList []string) ([]*regexp.Regexp, error) {
	regexes := make([]*regexp.Regexp, 0, len(regexList))
	for _, r := range regexList {
		regex, err := regexp.Compile(r)
		if err != nil {
			return nil, fmt.Errorf("invalid metric regex %s: %w", r, err)
		}
		regexes = append(regexes, regex)
	}
	return regexes, nil
}

func getRegexMappings(mappings map[string]string) (map[*regexp.Regexp]*regexp.Regexp, error) {
	regexMappings := make(map[*regexp.Regexp]*regexp.Regexp)
	for k, v := range mappings {
		kRegex, err := regexp.Compile(k)
		if err != nil {
			return nil, fmt.Errorf("invalid metric regex %s: %w", k, err)
		}

		vRegex, err := regexp.Compile(v)
		if err != nil {
			return nil, fmt.Errorf("invalid label regex %s: %w", v, err)
		}
		regexMappings[kRegex] = vRegex
	}
	return regexMappings, nil
}

func getLabelMappings(mappings []LabelMatcher) (map[*regexp.Regexp]*regexp.Regexp, error) {
	regexMappings := make(map[*regexp.Regexp]*regexp.Regexp)
	for _, mapping := range mappings {
		kRegex, err := regexp.Compile(mapping.LabelRegex)
		if err != nil {
			return nil, fmt.Errorf("invalid metric regex %s: %w", mapping.LabelRegex, err)
		}

		vRegex, err := regexp.Compile(mapping.ValueRegex)
		if err != nil {
			return nil, fmt.Errorf("invalid label regex %s: %w", mapping.ValueRegex, err)
		}
		regexMappings[kRegex] = vRegex
	}
	return regexMappings, nil
}

type FluentForwardLogExport struct {
	Name         string `toml:"name" comment:"Name of the exporter."`
	Destination  string `toml:"destination" comment:"Fluentd/Fluent Bit endpoint to send logs to. Must be in the form tcp://<host>:<port> or unix:///path/to/socket."`
	TagAttribute string `toml:"tag-attribute" comment:"Attribute key to use as the tag for the log. If the attribute is not set, the log will be ignored by this exporter."`
}

func (f *FluentForwardLogExport) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("name must be set")
	}
	if f.Destination == "" {
		return fmt.Errorf("destination must be set")
	}

	if strings.HasPrefix(f.Destination, "unix://") {
		socketPath := strings.TrimPrefix(f.Destination, "unix://")
		if len(socketPath) == 0 {
			return fmt.Errorf("invalid destination %s: unix socket path is empty", f.Destination)
		}
	} else if strings.HasPrefix(f.Destination, "tcp://") {
		destinationHostPort := strings.TrimPrefix(f.Destination, "tcp://")
		_, port, err := net.SplitHostPort(destinationHostPort)
		if err != nil {
			return fmt.Errorf("invalid destination %s: %w", f.Destination, err)
		}

		numericPort, err := strconv.ParseInt(port, 10, 0)
		if err != nil {
			return fmt.Errorf("invalid destination %s: unable to parse %s as integer", f.Destination, port)
		}
		if numericPort < 1 || numericPort > 65535 {
			return fmt.Errorf("invalid destination %s: port %d not between 1 and 65535", f.Destination, numericPort)
		}
	} else {
		return fmt.Errorf("invalid destination %s: must be in the form tcp://<host>:<port> or unix:///path/to/socket", f.Destination)
	}

	if f.TagAttribute == "" {
		return fmt.Errorf("tag-attribute must be set")
	}
	return nil
}
