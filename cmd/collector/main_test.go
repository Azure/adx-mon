package main

import (
	"os/exec"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector"

	"github.com/Azure/adx-mon/cmd/collector/config"
	"github.com/Azure/adx-mon/collector/metadata"
	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
)

func TestMainFunction_Config(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "config")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run main function: %v\nOutput: %s", err, string(output))
	}

	var fileConfig config.Config
	require.NoError(t, toml.Unmarshal(output, &fileConfig))
}

// Ensure that the value returned from newLogLabeler is nil when assigned to the interface type metadata.LogLabeler.
// Needed to avoid segfaults.
func TestNewLogLabelerNilKubeNode(t *testing.T) {
	var labeler metadata.LogLabeler = newLogLabeler(nil)
	require.Nil(t, labeler)
}

// Ensure that the value returned from newLogLabeler is nil when assigned to the interface type metadata.LogLabeler.
// Needed to avoid segfaults.
func TestNewMetricLabelerNilKubeNode(t *testing.T) {
	var labeler metadata.MetricLabeler = newMetricLabeler(nil)
	require.Nil(t, labeler)
}

func TestStaticScrapeTargets(t *testing.T) {
	var cfg config.Config
	require.NoError(t, toml.Unmarshal([]byte(`[prometheus-scrape]
database = 'Metrics'
scrape-interval = 30
static-scrape-target = [
  { host-regex = '.*', url = 'http://node:3100/metrics', namespace = 'adx-mon', pod = 'collector', container = 'collector', scrape-interval = '60s' },
  { host-regex = '^node$', url = 'https://node:10250/metrics', namespace = 'kube-system', pod = 'kubelet', container = 'kubelet' },
  { host-regex = '^other$', url = 'http://other/metrics', namespace = 'other', pod = 'other', container = 'other', scrape-interval = '5s' }
]`), &cfg))
	require.NoError(t, cfg.PrometheusScrape.Validate())
	targets, err := staticScrapeTargets(cfg.PrometheusScrape.StaticScrapeTarget, "node")
	require.NoError(t, err)
	require.Equal(t, []collector.ScrapeTarget{
		{Static: true, Addr: "http://node:3100/metrics", Namespace: "adx-mon", Pod: "collector", Container: "collector", ScrapeInterval: time.Minute},
		{Static: true, Addr: "https://node:10250/metrics", Namespace: "kube-system", Pod: "kubelet", Container: "kubelet"},
	}, targets)

	// Changing only the interval preserves all static target identity fields.
	cfg.PrometheusScrape.StaticScrapeTarget[0].ScrapeInterval = nil
	inherited, err := staticScrapeTargets(cfg.PrometheusScrape.StaticScrapeTarget, "node")
	require.NoError(t, err)
	targets[0].ScrapeInterval = 0
	require.Equal(t, targets, inherited)

	cfg.PrometheusScrape.StaticScrapeTarget[0].HostRegex = "["
	_, err = staticScrapeTargets(cfg.PrometheusScrape.StaticScrapeTarget, "node")
	require.ErrorContains(t, err, "failed to match hostname node")
}
