package main

import (
	"errors"
	"fmt"
	"os"
)

var homedir string

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
		StaticScrapeTarget:    []ScrapeTarget{},
		ScrapeIntervalSeconds: 30,
	},

	PrometheusRemoteWrite: []PrometheusRemoteWrite{
		{
			Path:      "/remote_write",
			AddLabels: make(map[string]string),
		},
	},
	OtelLog: OtelLog{
		AddAttributes: make(map[string]string),
	},
}

type Config struct {
	Endpoint           string `toml:"endpoint" comment:"Ingestor URL to send collected telemetry."`
	Hostname           string `toml:"hostname" comment:"Hostname filter override."`
	InsecureSkipVerify bool   `toml:"insecure-skip-verify" comment:"Skip TLS verification."`
	ListenAddr         string `toml:"listen-addr" comment:"Address to listen on for endpoints."`
	MaxBatchSize       int    `toml:"max-batch-size" comment:"Maximum number of samples to send in a single batch."`
	StorageDir         string `toml:"storage-dir" comment:"Storage directory for the WAL."`

	// These are global config options that apply to all endpoints.
	AddLabels                map[string]string `toml:"add-labels" comment:"Key/value pairs of labels to add to all metrics."`
	DropLabels               map[string]string `toml:"drop-labels" comment:"Labels to drop if they match a metrics regex in the format <metrics regex>=<label name>.  These are dropped from all metrics collected by this agent"`
	DropMetrics              []string          `toml:"drop-metrics" comment:"Regexes of metrics to drop."`
	DisableMetricsForwarding bool              `toml:"disable-metrics-forwarding" comment:"Disable metrics forwarding to endpoints."`

	// TODO: Move these to endpoint specific config(?).
	AddAttributes  map[string]string `toml:"add-attributes" comment:"Key/value pairs of attributes to add to all logs."`
	LiftAttributes []string          `toml:"lift-attributes" comment:"Attributes lifted from the Body and added to Attributes."`

	PrometheusScrape      *PrometheusScrape       `toml:"prometheus-scrape" comment:"Defines a prometheus scrape endpoint."`
	PrometheusRemoteWrite []PrometheusRemoteWrite `toml:"prometheus-remote-write" comment:"Defines a prometheus remote write endpoint."`
	OtelLog               OtelLog                 `toml:"otel-log" comment:"Defines an OpenTelemetry log endpoint."`
}

type PrometheusScrape struct {
	Database                 string         `toml:"database" comment:"Database to store metrics in."`
	StaticScrapeTarget       []ScrapeTarget `toml:"static-scrape-target" comment:"Defines a static scrape target."`
	ScrapeIntervalSeconds    int            `toml:"scrape-interval" comment:"Scrape interval in seconds."`
	DisableMetricsForwarding bool           `toml:"disable-metrics-forwarding" comment:"Disable metrics forwarding to endpoints."`

	AddLabels   map[string]string `toml:"add-labels" comment:"Key/value pairs of labels to add to all metrics."`
	DropLabels  map[string]string `toml:"drop-labels" comment:"Labels to drop if they match a metrics regex in the format <metrics regex>=<label name>.  These are dropped from all metrics collected by this agent"`
	DropMetrics []string          `toml:"drop-metrics" comment:"Regexes of metrics to drop."`
}

func (s PrometheusScrape) Validate() error {
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

type ScrapeTarget struct {
	HostRegex string `toml:"host-regex" comment:"The regex to match the host name against.  If the hostname matches, the URL will be scraped."`
	URL       string `toml:"url" comment:"The URL to scrape."`
	Namespace string `toml:"namespace" comment:"The namespace label to add for metrics scraped at this URL."`
	Pod       string `toml:"pod" comment:"The pod label to add for metrics scraped at this URL."`
	Container string `toml:"container" comment:"The container label to add for metrics scraped at this URL."`
}

func (t ScrapeTarget) Validate() error {
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
	Database    string            `toml:"database" comment:"Database to store metrics in."`
	Path        string            `toml:"path" comment:"The path to listen on for prometheus remote write requests.  Defaults to /receive."`
	AddLabels   map[string]string `toml:"add-labels" comment:"Key/value pairs of labels to add to all metrics."`
	DropLabels  map[string]string `toml:"drop-labels" comment:"Labels to drop if they match a metrics regex in the format <metrics regex>=<label name>.  These are dropped from all metrics collected by this agent"`
	DropMetrics []string          `toml:"drop-metrics" comment:"Regexes of metrics to drop."`

	DisableMetricsForwarding bool `toml:"disable-metrics-forwarding" comment:"Disable metrics forwarding to endpoints."`
}

func (w PrometheusRemoteWrite) Validate() error {
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

	return nil
}

type OtelLog struct {
	AddAttributes  map[string]string `toml:"add-attributes" comment:"Key/value pairs of attributes to add to all logs."`
	LiftAttributes []string          `toml:"lift-attributes" comment:"Attributes lifted from the Body and added to Attributes."`
}

func (w OtelLog) Validate() error {
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

func (c Config) Validate() error {
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

	if err := c.OtelLog.Validate(); err != nil {
		return err
	}

	if c.PrometheusScrape != nil {
		if err := c.PrometheusScrape.Validate(); err != nil {
			return err
		}
	}

	return nil
}
