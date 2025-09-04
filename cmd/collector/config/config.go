package config

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/Azure/adx-mon/collector/logs/sources/tail/sourceparse"
	"github.com/Azure/adx-mon/collector/logs/transforms"
	"github.com/Azure/adx-mon/collector/logs/transforms/parser"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/siderolabs/go-kmsg"
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
	Endpoint:                     "https://ingestor.adx-mon.svc.cluster.local",
	MaxBatchSize:                 5000,
	WALFlushIntervalMilliSeconds: 100,
	ListenAddr:                   ":8080",
	StorageDir:                   homedir,
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
	HostLog: []*HostLog{
		{
			StaticFileTargets: []*TailTarget{},
			JournalTargets:    []*JournalTarget{},
			AddAttributes:     make(map[string]string),
		},
	},
}

type Config struct {
	Endpoint           string `toml:"endpoint,omitempty" comment:"Ingestor URL to send collected telemetry."`
	Kubeconfig         string `toml:"kube-config,omitempty" comment:"Path to kubernetes client config"`
	InsecureSkipVerify bool   `toml:"insecure-skip-verify,omitempty" comment:"Skip TLS verification."`
	ListenAddr         string `toml:"listen-addr,omitempty" comment:"Address to listen on for endpoints."`
	Region             string `toml:"region,omitempty" comment:"Region is a location identifier."`
	TLSKeyFile         string `toml:"tls-key-file,omitempty" comment:"Optional path to the TLS key file."`
	TLSCertFile        string `toml:"tls-cert-file,omitempty" comment:"Optional path to the TLS cert bundle file."`

	MaxConnections               int   `toml:"max-connections,omitempty" comment:"Maximum number of connections to accept."`
	MaxBatchSize                 int   `toml:"max-batch-size,omitempty" comment:"Maximum number of samples to send in a single batch."`
	MaxSegmentAgeSeconds         int   `toml:"max-segment-age-seconds,omitempty" comment:"Max segment agent in seconds."`
	MaxSegmentSize               int64 `toml:"max-segment-size,omitempty" comment:"Maximum segment size in bytes."`
	MaxDiskUsage                 int64 `toml:"max-disk-usage,omitempty" comment:"Maximum allowed size in bytes of all segments on disk."`
	WALFlushIntervalMilliSeconds int   `toml:"wal-flush-interval-ms,omitempty" comment:"Interval to flush the WAL. (default 100)"`
	MaxTransferConcurrency       int   `toml:"max-transfer-concurrency,omitempty" comment:"Maximum number of concurrent transfers."`

	StorageDir  string `toml:"storage-dir,omitempty" comment:"Storage directory for the WAL and log cursors."`
	EnablePprof bool   `toml:"enable-pprof,omitempty" comment:"Enable pprof endpoints."`

	// These are global config options that apply to all endpoints.
	DefaultDropMetrics        *bool             `toml:"default-drop-metrics,omitempty" comment:"Default to dropping all metrics.  Only metrics matching a keep rule will be kept."`
	AddLabels                 map[string]string `toml:"add-labels,omitempty" comment:"Global Key/value pairs of labels to add to all metrics."`
	DropLabels                map[string]string `toml:"drop-labels,omitempty" comment:"Global labels to drop if they match a metrics regex in the format <metrics regex>=<label name>. These are dropped from all metrics collected by this agent"`
	DropMetrics               []string          `toml:"drop-metrics,omitempty" comment:"Global Regexes of metrics to drop."`
	KeepMetrics               []string          `toml:"keep-metrics,omitempty" comment:"Global Regexes of metrics to keep."`
	KeepMetricsWithLabelValue []LabelMatcher    `toml:"keep-metrics-with-label-value,omitempty" comment:"Global Regexes of metrics to keep if they have the given label and value. These are kept from all metrics collected by this agent"`
	LiftLabels                []*LiftLabel      `toml:"lift-labels,omitempty" comment:"Global labels to lift from the metric to top level columns"`

	DisableMetricsForwarding bool `toml:"disable-metrics-forwarding,omitempty" comment:"Disable metrics forwarding to endpoints."`
	DisableGzip              bool `toml:"disable-gzip,omitempty" comment:"Disable gzip compression for transferring segments."`

	// These are global config options that apply to all endpoints.
	AddAttributes  map[string]string `toml:"add-attributes,omitempty" comment:"Key/value pairs of attributes to add to all logs."`
	LiftAttributes []string          `toml:"lift-attributes,omitempty" comment:"Attributes lifted from the Body field and added to Attributes."`
	LiftResources  []*LiftResource   `toml:"lift-resources,omitempty" comment:"Fields lifted from the Resource and added as top level columns."`

	PrometheusScrape      *PrometheusScrape        `toml:"prometheus-scrape,omitempty" comment:"Defines a prometheus format endpoint scraper."`
	PrometheusRemoteWrite []*PrometheusRemoteWrite `toml:"prometheus-remote-write,omitempty" comment:"Defines a prometheus remote write endpoint."`
	OtelLog               *OtelLog                 `toml:"otel-log,omitempty" comment:"Defines an OpenTelemetry log endpoint. Accepts OTLP/HTTP."`
	OtelMetric            []*OtelMetric            `toml:"otel-metric,omitempty" comment:"Defines an OpenTelemetry metric endpoint. Accepts OTLP/HTTP and/or OTLP/gRPC."`
	HostLog               []*HostLog               `toml:"host-log,omitempty" comment:"Defines a host log scraper."`
	Exporters             *Exporters               `toml:"exporters,omitempty" comment:"Optional configuration for exporting telemetry outside of adx-mon in parallel with sending to ADX.\nExporters are declared here and referenced by name in each collection source."`

	// LogMonitor defines optional allow-list of database:table pairs for loss visibility counting.
	LogMonitor *LogMonitor `toml:"log_monitor" comment:"Optional configuration for monitored log database:table pairs used for loss visibility. Each pair is <database>:<table>. Invalid entries are skipped with a warning."`
}

// MaxMonitoredPairsWarn is a soft cap to warn operators of potential excessive cardinality.
const MaxMonitoredPairsWarn = 500

// LogMonitor tracks a set of allowed <database>:<table> pairs for loss visibility counting.
// It is immutable after the first BuildSet() call; subsequent changes to Pairs are not observed.
// For dynamic reconfiguration, construct a new LogMonitor instance.
type LogMonitor struct {
	Pairs  []string            `toml:"pairs" comment:"List of database:table pairs to monitor for loss visibility."`
	parsed map[string]struct{} `toml:"-"`
	once   sync.Once           `toml:"-"`
}

func (lm *LogMonitor) monitorKey(db, table string) string { return db + "\x00" + table }

// BuildSet parses and validates pairs into the internal set. It is idempotent and thread-safe.
func (lm *LogMonitor) BuildSet() {
	if lm == nil {
		return
	}
	lm.once.Do(func() {
		lm.parsed = make(map[string]struct{})
		for _, raw := range lm.Pairs {
			p := strings.TrimSpace(raw)
			if p == "" {
				logger.Warn("log_monitor: empty pair entry skipped")
				continue
			}
			db, tbl, ok := strings.Cut(p, ":")
			if !ok {
				logger.Warn("log_monitor: invalid pair skipped", "value", p)
				continue
			}
			db = strings.TrimSpace(db)
			tbl = strings.TrimSpace(tbl)
			if db == "" || tbl == "" {
				logger.Warn("log_monitor: invalid pair skipped", "value", p)
				continue
			}
			lm.parsed[lm.monitorKey(db, tbl)] = struct{}{}
		}
		if len(lm.parsed) > MaxMonitoredPairsWarn {
			logger.Warn("log_monitor: high monitored pair count", "count", len(lm.parsed))
		}
	})
}

// IsMonitored returns true if db/table is in the allow-list.
// Note: This method assumes BuildSet has already been called.
func (lm *LogMonitor) IsMonitored(db, table string) bool {
	if lm == nil {
		return false
	}
	// No lazy initialization here to avoid racy map writes; BuildSet must be invoked during setup.
	_, ok := lm.parsed[lm.monitorKey(db, table)]
	return ok
}

// NewLogMonitor constructs a LogMonitor and eagerly builds the internal set.
func NewLogMonitor(pairs []string) *LogMonitor {
	lm := &LogMonitor{Pairs: pairs}
	lm.BuildSet()
	return lm
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
	DropLabels                map[string]string `toml:"drop-labels" comment:"Labels to drop if they match a metrics regex in the format <metrics regex>=<label name>."`
	DropMetrics               []string          `toml:"drop-metrics" comment:"Regexes of metrics to drop."`
	KeepMetrics               []string          `toml:"keep-metrics" comment:"Regexes of metrics to keep."`
	KeepMetricsWithLabelValue []LabelMatcher    `toml:"keep-metrics-with-label-value" comment:"Regexes of metrics to keep if they have the given label and value."`

	Exporters []string `toml:"exporters" comment:"List of exporter names to forward metrics to."`
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
	LabelRegex string `toml:"label-regex," comment:"The regex to match the label value against.  If the label value matches, the metric will be kept."`
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
	DropLabels                map[string]string `toml:"drop-labels" comment:"Labels to drop if they match a metrics regex in the format <metrics regex>=<label name>."`
	DropMetrics               []string          `toml:"drop-metrics" comment:"Regexes of metrics to drop."`
	KeepMetrics               []string          `toml:"keep-metrics" comment:"Regexes of metrics to keep."`
	KeepMetricsWithLabelValue []LabelMatcher    `toml:"keep-metrics-with-label-value" comment:"Regexes of metrics to keep if they have the given label and value."`

	Exporters []string `toml:"exporters" comment:"List of exporter names to forward metrics to."`
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
	Transforms     []*LogTransform   `toml:"transforms" comment:"Defines a list of transforms to apply to log lines."`
	Exporters      []string          `toml:"exporters" comment:"List of exporter names to forward logs to."`
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

	for _, v := range w.Transforms {
		if !transforms.IsValidTransformType(v.Name) {
			return fmt.Errorf("otel-log.transforms %s is not a valid transform", v.Name)
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
	KeepMetricsWithLabelValue []LabelMatcher    `toml:"keep-metrics-with-label-value" comment:"Regexes of metrics to keep if they have the given label and value."`

	Exporters []string `toml:"exporters" comment:"List of exporter names to forward metrics to."`
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

type HostLog struct {
	DisableKubeDiscovery bool              `toml:"disable-kube-discovery" comment:"Disable discovery of Kubernetes pod targets. Only one HostLog configuration can use Kubernetes discovery."`
	AddAttributes        map[string]string `toml:"add-attributes" comment:"Key/value pairs of attributes to add to all logs."`
	StaticFileTargets    []*TailTarget     `toml:"file-target" comment:"Defines a tail file target."`
	StaticPodTargets     []*PodTarget      `toml:"static-pod-target" comment:"Defines a static Kubernetes pod target to scrape. These are pods managed by the Kubelet and not discoverable via the apiserver."`
	JournalTargets       []*JournalTarget  `toml:"journal-target" comment:"Defines a journal target to scrape."`
	KernelTargets        []*KernelTarget   `toml:"kernel-target" comment:"Defines a kernel target to scrape."`
	Transforms           []*LogTransform   `toml:"transforms" comment:"Defines a list of transforms to apply to log lines."`
	Exporters            []string          `toml:"exporters" comment:"List of exporter names to forward logs to."`
}

func (w *HostLog) Validate() error {
	for k, v := range w.AddAttributes {
		if k == "" {
			return errors.New("host-log.add-attributes key must be set")
		}
		if v == "" {
			return errors.New("host-log.add-attributes value must be set")
		}
	}

	// Empty is ok - defaults to plain
	validLogTypes := []sourceparse.Type{sourceparse.LogTypeDocker, sourceparse.LogTypeCri, sourceparse.LogTypeKubernetes, sourceparse.LogTypePlain, ""}

	for _, v := range w.StaticFileTargets {
		if v.FilePath == "" {
			return errors.New("host-log.file-target.file-path must be set")
		}
		if v.Database == "" {
			return errors.New("host-log.file-target.database must be set")
		}
		if v.Table == "" {
			return errors.New("host-log.file-target.table must be set")
		}

		foundValidType := false
		for _, validType := range validLogTypes {
			if v.LogType == validType {
				foundValidType = true
				break
			}
		}
		if !foundValidType {
			return fmt.Errorf("host-log.file-target.log-type %s is not a valid log type", v.LogType)
		}

		for _, parserName := range v.Parsers {
			if !parser.IsValidParser(parserName) {
				return fmt.Errorf("host-log.file-target.parsers %s is not a valid parser", parserName)
			}
		}
	}

	for _, v := range w.JournalTargets {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("host-log.journal-target.%w", err)
		}
	}

	for _, v := range w.KernelTargets {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("host-log.kernel-target.%w", err)
		}
	}

	for _, v := range w.Transforms {
		if !transforms.IsValidTransformType(v.Name) {
			return fmt.Errorf("host-log.transforms %s is not a valid transform", v.Name)
		}
	}

	return nil
}

type JournalTarget struct {
	Matches       []string `toml:"matches" comment:"Matches for the journal reader based on journalctl MATCHES. To select a systemd unit, use the field _SYSTEMD_UNIT. (e.g. '_SYSTEMD_UNIT=avahi-daemon.service' for selecting logs from the avahi-daemon service.)"`
	Database      string   `toml:"database" comment:"Database to store logs in."`
	Table         string   `toml:"table" comment:"Table to store logs in."`
	Parsers       []string `toml:"parsers" comment:"Parsers to apply sequentially to the log line."`
	JournalFields []string `toml:"journal-fields" comment:"Optional journal metadata fields http://www.freedesktop.org/software/systemd/man/systemd.journal-fields.html"`
}

func (j *JournalTarget) Validate() error {
	if j.Database == "" {
		return errors.New("database must be set")
	}
	if j.Table == "" {
		return errors.New("table must be set")
	}

	haveFirstFilter := false
	for _, filter := range j.Matches {
		if filter == "" {
			return errors.New("match must have a value")
		}

		if filter == "+" {
			if !haveFirstFilter {
				return errors.New("matches must not start with +")
			}
			continue
		}

		splitVals := strings.Split(filter, "=")
		if len(splitVals) != 2 {
			return fmt.Errorf("match %s must be in the format key=value", filter)
		}
		if splitVals[0] == "" {
			return fmt.Errorf("match %s must have a key", filter)
		}
		if splitVals[1] == "" {
			return fmt.Errorf("match %s must have a value", filter)
		}
		haveFirstFilter = true
	}

	for _, parserName := range j.Parsers {
		if !parser.IsValidParser(parserName) {
			return fmt.Errorf("parsers %s is not a valid parser", parserName)
		}
	}

	return nil
}

type TailTarget struct {
	FilePath string           `toml:"file-path" comment:"The path to the file to tail."`
	LogType  sourceparse.Type `toml:"log-type" comment:"The type of log being output. This defines how timestamps and log messages are extracted from structured log types like docker json files. Options are: docker, cri, kubernetes, plain, or unset."`
	Database string           `toml:"database" comment:"Database to store logs in."`
	Table    string           `toml:"table" comment:"Table to store logs in."`
	Parsers  []string         `toml:"parsers" comment:"Parsers to apply sequentially to the log line."`
}

type KernelTarget struct {
	Database string `toml:"database" comment:"Database to store logs in."`
	Table    string `toml:"table" comment:"Table to store logs in."`
	Priority string `toml:"priority" comment:"One of emerg, alert, crit, err, warning, notice, info, debug, default is info."`
}

func (k *KernelTarget) Validate() error {
	if k.Database == "" {
		return errors.New("database must be set")
	}
	if k.Table == "" {
		return errors.New("table must be set")
	}
	if k.Priority == "" {
		k.Priority = "info"
	}
	// Create a map of valid priorities from the kmsg package
	validPriorities := make(map[string]bool)
	for i := kmsg.Emerg; i <= kmsg.Debug; i++ {
		validPriorities[i.String()] = true
	}

	if !validPriorities[k.Priority] {
		return fmt.Errorf("priority %q is not valid, must be one of: emerg, alert, crit, err, warning, notice, info, debug",
			k.Priority)
	}
	return nil
}

type PodTarget struct {
	Namespace    string            `toml:"namespace" comment:"The namespace of the pod to scrape."`
	Name         string            `toml:"name" comment:"The name of the pod to scrape."`
	LabelTargets map[string]string `toml:"label-targets" comment:"The labels to match on the pod.  If the pod has all of these labels, it will be scraped."`
	Destination  string            `toml:"destination" comment:"The destination to send the logs to.  Syntax matches that of adx-mon/log-destination annotations."`
	Parsers      []string          `toml:"parsers" comment:"Parsers to apply sequentially to the log line."`
}

type LogTransform struct {
	Name   string         `toml:"name" comment:"The name of the transform to apply to the log line."`
	Config map[string]any `toml:"config" comment:"The configuration for the transform."`
}

func (c *Config) Validate() error {
	if c.Exporters != nil {
		if err := c.Exporters.Validate(); err != nil {
			return err
		}
	}

	// Eagerly build log monitor set to avoid racy lazy initialization by concurrent sinks.
	if c.LogMonitor != nil {
		c.LogMonitor.BuildSet()
	}

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

		for _, exporterName := range v.Exporters {
			if !HasMetricsExporter(exporterName, c.Exporters) {
				return fmt.Errorf("prometheus-remote-write.exporters %q not found", exporterName)
			}
		}
	}

	if c.OtelLog != nil {
		if err := c.OtelLog.Validate(); err != nil {
			return err
		}

		for _, exporterName := range c.OtelLog.Exporters {
			if !HasLogsExporter(exporterName, c.Exporters) {
				return fmt.Errorf("otel-log.exporters %q not found", exporterName)
			}
		}
	}

	tailScrapingFromKube := false
	for i, v := range c.HostLog {
		if err := v.Validate(); err != nil {
			return err
		}
		if !v.DisableKubeDiscovery {
			if tailScrapingFromKube {
				return fmt.Errorf("host-log[%d].disable-kube-discovery not set for more than one HostLog configuration", i)
			}
			tailScrapingFromKube = true
		}

		for _, exporterName := range v.Exporters {
			if !HasLogsExporter(exporterName, c.Exporters) {
				return fmt.Errorf("host-log[%d].exporters %q not found", i, exporterName)
			}
		}
	}

	for i, v := range c.OtelMetric {
		if err := v.Validate(); err != nil {
			return err
		}

		if v.Path != "" {
			if _, ok := existingPaths[v.Path]; ok {
				return fmt.Errorf("otel-metric[%d].path %s is already defined", i, v.Path)
			}
			existingPaths[v.Path] = struct{}{}
		}

		for _, exporterName := range v.Exporters {
			if !HasMetricsExporter(exporterName, c.Exporters) {
				return fmt.Errorf("otel-metric[%d].exporters %q not found", i, exporterName)
			}
		}
	}

	if c.PrometheusScrape != nil {
		if err := c.PrometheusScrape.Validate(); err != nil {
			return err
		}

		for _, exporterName := range c.PrometheusScrape.Exporters {
			if !HasMetricsExporter(exporterName, c.Exporters) {
				return fmt.Errorf("prometheus-scrape.exporters %q not found", exporterName)
			}
		}
	}

	if c.WALFlushIntervalMilliSeconds == 0 {
		c.WALFlushIntervalMilliSeconds = DefaultConfig.WALFlushIntervalMilliSeconds
	} else if c.WALFlushIntervalMilliSeconds < 0 {
		return errors.New("wal-flush-interval must be greater than 0")
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
