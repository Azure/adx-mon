package main

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/Azure/adx-mon/collector/logs/sources/tail/sourceparse"
	"github.com/Azure/adx-mon/collector/logs/transforms"
	"github.com/Azure/adx-mon/collector/logs/transforms/parser"
)

var (
	homedir         string
	validColumnName = regexp.MustCompile(`^[a-zA-Z0-9]+$`)
)

func init() {
	homedir = os.Getenv("HOME")
	if homedir == "" {
		hd, err := os.UserHomeDir()
		if err != nil {
			homedir = os.TempDir()
		}
		homedir = hd
	}

	homedir = fmt.Sprintf("%s/.adx-mon/collector", homedir)
	DefaultConfig.StorageDir = homedir
}

var DefaultConfig = Config{
	MaxBatchSize: 5000,
	ListenAddr:   ":8080",
	StorageDir:   homedir,
	PrometheusScrape: &PrometheusScrape{
		StaticScrapeTarget:    []*ScrapeTarget{},
		ScrapeIntervalSeconds: 30,
	},

	PrometheusRemoteWrite: []*PrometheusRemoteWrite{
		{
			Path:      "/remote_write",
			AddLabels: make(map[string]string),
		},
	},
	OtelLog: &OtelLog{
		AddAttributes: make(map[string]string),
	},
	OtelMetric: []*OtelMetric{
		{
			Path:      "/v1/metrics",
			AddLabels: make(map[string]string),
		},
	},
	TailLog: []*TailLog{
		{
			StaticTailTarget: []*TailTarget{},
			AddAttributes:    make(map[string]string),
		},
	},
}

type Config struct {
	Endpoint           string `toml:"endpoint" comment:"Ingestor URL to send collected telemetry."`
	Kubeconfig         string `toml:"kube-config" comment:"Path to kubernetes client config"`
	InsecureSkipVerify bool   `toml:"insecure-skip-verify" comment:"Skip TLS verification."`
	ListenAddr         string `toml:"listen-addr" comment:"Address to listen on for endpoints."`
	Region             string `toml:"region" comment:"Region is a location identifier."`
	TLSKeyFile         string `toml:"tls-key-file" comment:"Optional path to the TLS key file."`
	TLSCertFile        string `toml:"tls-cert-file" comment:"Optional path to the TLS cert bundle file."`

	MaxConnections       int   `toml:"max-connections" comment:"Maximum number of connections to accept."`
	MaxBatchSize         int   `toml:"max-batch-size" comment:"Maximum number of samples to send in a single batch."`
	MaxSegmentAgeSeconds int   `toml:"max-segment-age-seconds" comment:"Max segment agent in seconds."`
	MaxSegmentSize       int64 `toml:"max-segment-size" comment:"Maximum segment size in bytes."`
	MaxDiskUsage         int64 `toml:"max-disk-usage" comment:"Maximum allowed size in bytes of all segments on disk."`

	StorageDir  string `toml:"storage-dir" comment:"Storage directory for the WAL and log cursors."`
	EnablePprof bool   `toml:"enable-pprof" comment:"Enable pprof endpoints."`

	// These are global config options that apply to all endpoints.
	DefaultDropMetrics        *bool             `toml:"default-drop-metrics" comment:"Default to dropping all metrics.  Only metrics matching a keep rule will be kept."`
	AddLabels                 map[string]string `toml:"add-labels" comment:"Global Key/value pairs of labels to add to all metrics."`
	DropLabels                map[string]string `toml:"drop-labels" comment:"Global labels to drop if they match a metrics regex in the format <metrics regex>=<label name>.  These are dropped from all metrics collected by this agent"`
	DropMetrics               []string          `toml:"drop-metrics" comment:"Global Regexes of metrics to drop."`
	KeepMetrics               []string          `toml:"keep-metrics" comment:"Global Regexes of metrics to keep."`
	KeepMetricsWithLabelValue []LabelMatcher    `toml:"keep-metrics-with-label-value" comment:"Global Regexes of metrics to keep if they have the given label and value in the format <metrics regex>=<label name>=<label value>.  These are kept from all metrics collected by this agent"`
	LiftLabels                []*LiftLabel      `toml:"lift-labels" comment:"Global labels to lift from the metric to top level columns"`

	DisableMetricsForwarding bool `toml:"disable-metrics-forwarding" comment:"Disable metrics forwarding to endpoints."`

	// These are global config options that apply to all endpoints.
	AddAttributes  map[string]string `toml:"add-attributes" comment:"Key/value pairs of attributes to add to all logs."`
	LiftAttributes []string          `toml:"lift-attributes" comment:"Attributes lifted from the Body and added to Attributes."`
	LiftResources  []*LiftResource   `toml:"lift-resources" comment:"Fields lifted from the Resource and added as top level columns."`

	PrometheusScrape      *PrometheusScrape        `toml:"prometheus-scrape" comment:"Defines a prometheus scrape endpoint."`
	PrometheusRemoteWrite []*PrometheusRemoteWrite `toml:"prometheus-remote-write" comment:"Defines a prometheus remote write endpoint."`
	OtelLog               *OtelLog                 `toml:"otel-log" comment:"Defines an OpenTelemetry log endpoint. Accepts OTLP/HTTP."`
	OtelMetric            []*OtelMetric            `toml:"otel-metric" comment:"Defines an OpenTelemetry metric endpoint. Optionally accepts OTLP/HTTP and/or OTLP/gRPC."`
	TailLog               []*TailLog               `toml:"tail-log" comment:"Defines a tail log scraper."`
}

type PrometheusScrape struct {
	Database                 string          `toml:"database" comment:"Database to store metrics in."`
	StaticScrapeTarget       []*ScrapeTarget `toml:"static-scrape-target" comment:"Defines a static scrape target."`
	ScrapeIntervalSeconds    int             `toml:"scrape-interval" comment:"Scrape interval in seconds."`
	ScrapeTimeout            int             `toml:"scrape-timeout" comment:"Scrape timeout in seconds."`
	DisableMetricsForwarding bool            `toml:"disable-metrics-forwarding" comment:"Disable metrics forwarding to endpoints."`
	DisableDiscovery         bool            `toml:"disable-discovery" comment:"Disable discovery of kubernetes pod targets."`

	DefaultDropMetrics        *bool             `toml:"default-drop-metrics" comment:"Default to dropping all metrics.  Only metrics matching a keep rule will be kept."`
	AddLabels                 map[string]string `toml:"add-labels" comment:"Key/value pairs of labels to add to all metrics."`
	DropLabels                map[string]string `toml:"drop-labels" comment:"Labels to drop if they match a metrics regex in the format <metrics regex>=<label name>.  These are dropped from all metrics collected by this agent"`
	DropMetrics               []string          `toml:"drop-metrics" comment:"Regexes of metrics to drop."`
	KeepMetrics               []string          `toml:"keep-metrics" comment:"Global Regexes of metrics to keep."`
	KeepMetricsWithLabelValue []LabelMatcher    `toml:"keep-metrics-with-label-value" comment:"Global Regexes of metrics to keep if they have the given label and value in the format <metrics regex>=<label name>=<label value>.  These are kept from all metrics collected by this agent"`
}

func (s *PrometheusScrape) Validate() error {
	if s.Database == "" {
		return errors.New("prom-scrape.database must be set")
	}

	for i, v := range s.StaticScrapeTarget {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("prom-scrape.static-scrape-target[%d].%w", i, err)
		}
	}

	if s.ScrapeIntervalSeconds <= 0 {
		return errors.New("prom-scrape.scrape-interval must be greater than 0")
	}
	return nil
}

type LabelMatcher struct {
	LabelRegex string `toml:"label-regex" comment:"The regex to match the label value against.  If the label value matches, the metric will be kept."`
	ValueRegex string `toml:"value-regex" comment:"The regex to match the label value against.  If the label value matches, the metric will be kept."`
}

type ScrapeTarget struct {
	HostRegex string `toml:"host-regex" comment:"The regex to match the host name against.  If the hostname matches, the URL will be scraped."`
	URL       string `toml:"url" comment:"The URL to scrape."`
	Namespace string `toml:"namespace" comment:"The namespace label to add for metrics scraped at this URL."`
	Pod       string `toml:"pod" comment:"The pod label to add for metrics scraped at this URL."`
	Container string `toml:"container" comment:"The container label to add for metrics scraped at this URL."`
}

type LiftLabel struct {
	Name       string `toml:"name" comment:"The name of the label to lift."`
	ColumnName string `toml:"column" comment:"The name of the column to lift the label to."`
}

type LiftResource struct {
	Name       string `toml:"name" comment:"The name of the resource to lift."`
	ColumnName string `toml:"column" comment:"The name of the column to lift the resource to."`
}

func (t *ScrapeTarget) Validate() error {
	if t.HostRegex == "" {
		return errors.New("host-regex must be set")
	}
	if t.URL == "" {
		return errors.New("url must be set")
	}
	if t.Namespace == "" {
		return errors.New("namespace must be set")
	}
	if t.Pod == "" {
		return errors.New("pod must be set")
	}
	if t.Container == "" {
		return errors.New("container must be set")
	}
	return nil
}

type PrometheusRemoteWrite struct {
	Database                 string `toml:"database" comment:"Database to store metrics in."`
	Path                     string `toml:"path" comment:"The path to listen on for prometheus remote write requests.  Defaults to /receive."`
	DisableMetricsForwarding *bool  `toml:"disable-metrics-forwarding" comment:"Disable metrics forwarding to endpoints."`

	DefaultDropMetrics        *bool             `toml:"default-drop-metrics" comment:"Default to dropping all metrics.  Only metrics matching a keep rule will be kept."`
	AddLabels                 map[string]string `toml:"add-labels" comment:"Key/value pairs of labels to add to all metrics."`
	DropLabels                map[string]string `toml:"drop-labels" comment:"Labels to drop if they match a metrics regex in the format <metrics regex>=<label name>.  These are dropped from all metrics collected by this agent"`
	DropMetrics               []string          `toml:"drop-metrics" comment:"Regexes of metrics to drop."`
	KeepMetrics               []string          `toml:"keep-metrics" comment:"Regexes of metrics to keep."`
	KeepMetricsWithLabelValue []LabelMatcher    `toml:"keep-metrics-with-label-value" comment:"Regexes of metrics to keep if they have the given label and value in the format <metrics regex>=<label name>=<label value>.  These are kept from all metrics collected by this agent"`
}

func (w *PrometheusRemoteWrite) Validate() error {
	if w.Path == "" {
		return errors.New("prometheus-remote-write.path must be set")
	}

	if w.Database == "" {
		return errors.New("prometheus-remote-write.database must be set")
	}

	for k, v := range w.AddLabels {
		if k == "" {
			return errors.New("prometheus-remote-write.add-labels key must be set")
		}
		if v == "" {
			return errors.New("prometheus-remote-write.add-labels value must be set")
		}
	}

	for k, v := range w.DropLabels {
		if k == "" {
			return errors.New("prometheus-remote-write.drop-labels key must be set")
		}
		if v == "" {
			return errors.New("prometheus-remote-write.drop-labels value must be set")
		}
	}

	for _, v := range w.KeepMetricsWithLabelValue {
		if v.LabelRegex == "" {
			return errors.New("prometheus-remote-write.keep-metrics-with-label-value key must be set")
		}

		if v.ValueRegex == "" {
			return errors.New("prometheus-remote-write.keep-metrics-with-label-value value must be set")
		}
	}

	return nil
}

type OtelLog struct {
	AddAttributes  map[string]string `toml:"add-attributes" comment:"Key/value pairs of attributes to add to all logs."`
	LiftAttributes []string          `toml:"lift-attributes" comment:"Attributes lifted from the Body and added to Attributes."`
}

func (w *OtelLog) Validate() error {
	for k, v := range w.AddAttributes {
		if k == "" {
			return errors.New("otel-log.add-attributes key must be set")
		}
		if v == "" {
			return errors.New("otel-log.add-attributes value must be set")
		}
	}

	return nil
}

type OtelMetric struct {
	Database                 string `toml:"database" comment:"Database to store metrics in."`
	Path                     string `toml:"path" comment:"The path to listen on for OTLP/HTTP requests."`
	GrpcPort                 int    `toml:"grpc-port" comment:"The port to listen on for OTLP/gRPC requests."`
	DisableMetricsForwarding *bool  `toml:"disable-metrics-forwarding" comment:"Disable metrics forwarding to endpoints."`

	DefaultDropMetrics        *bool             `toml:"default-drop-metrics" comment:"Default to dropping all metrics.  Only metrics matching a keep rule will be kept."`
	AddLabels                 map[string]string `toml:"add-labels" comment:"Key/value pairs of labels to add to all metrics."`
	DropLabels                map[string]string `toml:"drop-labels" comment:"Labels to drop if they match a metrics regex in the format <metrics regex>=<label name>.  These are dropped from all metrics collected by this agent"`
	DropMetrics               []string          `toml:"drop-metrics" comment:"Regexes of metrics to drop."`
	KeepMetrics               []string          `toml:"keep-metrics" comment:"Regexes of metrics to keep."`
	KeepMetricsWithLabelValue []LabelMatcher    `toml:"keep-metrics-with-label-value" comment:"Regexes of metrics to keep if they have the given label and value in the format <metrics regex>=<label name>=<label value>.  These are kept from all metrics collected by this agent"`
}

func (w *OtelMetric) Validate() error {
	if w.Database == "" {
		return errors.New("otel-metric.database must be set")
	}

	if w.Path == "" && w.GrpcPort == 0 {
		return errors.New("otel-metric.path or otel-metric.grpc-port must be set")
	}

	if w.GrpcPort != 0 {
		if w.GrpcPort < 1 || w.GrpcPort > 65535 {
			return errors.New("otel-metric.grpc-port must be between 1 and 65535")
		}
	}

	for k, v := range w.AddLabels {
		if k == "" {
			return errors.New("otel-metric.add-labels key must be set")
		}
		if v == "" {
			return errors.New("otel-metric.add-labels value must be set")
		}
	}

	for k, v := range w.DropLabels {
		if k == "" {
			return errors.New("otel-metric.drop-labels key must be set")
		}
		if v == "" {
			return errors.New("otel-metric.drop-labels value must be set")
		}
	}

	for _, v := range w.KeepMetricsWithLabelValue {
		if v.LabelRegex == "" {
			return errors.New("otel-metric.keep-metrics-with-label-value label-regex must be set")
		}

		if v.ValueRegex == "" {
			return errors.New("otel-metric.keep-metrics-with-label-value value-regex must be set")
		}
	}

	return nil
}

type TailLog struct {
	DisableKubeDiscovery bool              `toml:"disable-kube-discovery" comment:"Disable discovery of kubernetes pod targets. Only one TailLog configuration can use kubernetes discovery."`
	AddAttributes        map[string]string `toml:"add-attributes" comment:"Key/value pairs of attributes to add to all logs."`
	StaticTailTarget     []*TailTarget     `toml:"static-target" comment:"Defines a static tail target."`
	Transforms           []*TailTransform  `toml:"transforms" comment:"Defines a list of transforms to apply to log lines."`
}

type TailTarget struct {
	FilePath string           `toml:"file-path" comment:"The path to the file to tail."`
	LogType  sourceparse.Type `toml:"log-type" comment:"The type of log being output. This defines how timestamps and log messages are extracted from structured log types like docker json files.  Options are: docker, plain."`
	Database string           `toml:"database" comment:"Database to store logs in."`
	Table    string           `toml:"table" comment:"Table to store logs in."`
	Parsers  []string         `toml:"parsers" comment:"Parsers to apply sequentially to the log line."`
}

type TailTransform struct {
	Name   string         `toml:"name" comment:"The name of the transform to apply to the log line."`
	Config map[string]any `toml:"config" comment:"The configuration for the transform."`
}

func (w *TailLog) Validate() error {
	for k, v := range w.AddAttributes {
		if k == "" {
			return errors.New("tail-log.add-attributes key must be set")
		}
		if v == "" {
			return errors.New("tail-log.add-attributes value must be set")
		}
	}

	// Empty is ok - defaults to plain
	validLogTypes := []sourceparse.Type{sourceparse.LogTypeDocker, sourceparse.LogTypeCri, sourceparse.LogTypeKubernetes, sourceparse.LogTypePlain, ""}

	for _, v := range w.StaticTailTarget {
		if v.FilePath == "" {
			return errors.New("tail-log.static-target.file-path must be set")
		}
		if v.Database == "" {
			return errors.New("tail-log.static-target.database must be set")
		}
		if v.Table == "" {
			return errors.New("tail-log.static-target.table must be set")
		}

		foundValidType := false
		for _, validType := range validLogTypes {
			if v.LogType == validType {
				foundValidType = true
				break
			}
		}
		if !foundValidType {
			return fmt.Errorf("tail-log.static-target.log-type %s is not a valid log type", v.LogType)
		}

		for _, parserName := range v.Parsers {
			if !parser.IsValidParser(parserName) {
				return fmt.Errorf("tail-log.static-target.parsers %s is not a valid parser", parserName)
			}
		}
	}

	for _, v := range w.Transforms {
		if !transforms.IsValidTransformType(v.Name) {
			return fmt.Errorf("tail-log.transforms %s is not a valid transform", v.Name)
		}
	}

	return nil
}

func (c *Config) Validate() error {
	tlsCertEmpty := c.TLSCertFile == ""
	tlsKeyEmpty := c.TLSKeyFile == ""
	if tlsCertEmpty != tlsKeyEmpty {
		return errors.New("tls-cert-file and tls-key-file must both be set or both be empty")
	}

	for _, v := range c.LiftLabels {
		if v.Name == "" {
			return errors.New("lift-labels.name must be set")
		}
		if v.ColumnName != "" {
			if !validColumnName.MatchString(v.ColumnName) {
				return fmt.Errorf("lift-labels.column-name `%s` contains invalid characters", v.ColumnName)
			}
		}
	}

	for _, v := range c.LiftResources {
		if v.Name == "" {
			return errors.New("lift-resources.name must be set")
		}
		if v.ColumnName != "" {
			if !validColumnName.MatchString(v.ColumnName) {
				return fmt.Errorf("lift-resources.column-name `%s` contains invalid characters", v.ColumnName)
			}
		}
	}

	existingPaths := make(map[string]struct{})
	for _, v := range c.PrometheusRemoteWrite {
		if err := v.Validate(); err != nil {
			return err
		}

		if _, ok := existingPaths[v.Path]; ok {
			return fmt.Errorf("prometheus-remote-write.path %s is already defined", v.Path)
		}
		existingPaths[v.Path] = struct{}{}
	}

	if c.OtelLog != nil {
		if err := c.OtelLog.Validate(); err != nil {
			return err
		}
	}

	tailScrapingFromKube := false
	for _, v := range c.TailLog {
		if err := v.Validate(); err != nil {
			return err
		}
		if !v.DisableKubeDiscovery {
			if tailScrapingFromKube {
				return errors.New("tail-log.disable-kube-discovery not set for more than one TailLog configuration")
			}
			tailScrapingFromKube = true
		}
	}

	for _, v := range c.OtelMetric {
		if err := v.Validate(); err != nil {
			return err
		}

		if v.Path != "" {
			if _, ok := existingPaths[v.Path]; ok {
				return fmt.Errorf("otel-metric.path %s is already defined", v.Path)
			}
			existingPaths[v.Path] = struct{}{}
		}
	}

	if c.PrometheusScrape != nil {
		if err := c.PrometheusScrape.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (c *Config) processStringFields(v reflect.Value, f func(string) string) {
	switch v.Kind() {
	case reflect.String:
		v.SetString(f(v.String()))
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			c.processStringFields(v.Field(i), f)
		}
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		v = v.Elem()
		c.processStringFields(v, f)
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			c.processStringFields(v.Index(i), f)
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			value := v.MapIndex(key)
			if value.Kind() == reflect.String {
				v.SetMapIndex(key, reflect.ValueOf(f(value.String())))
				continue
			}

			c.processStringFields(v.MapIndex(key), f)
		}
	}
}

// ReplaceVariable replaces all instances of the given variable with the given value.
func (c *Config) ReplaceVariable(variable, value string) {
	c.processStringFields(reflect.ValueOf(c), func(s string) string {
		return strings.ReplaceAll(s, variable, value)
	})
}
