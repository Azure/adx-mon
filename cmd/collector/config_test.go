package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig_ValidatePromRemoteWrite_PathRequired(t *testing.T) {
	c := Config{
		PrometheusRemoteWrite: []*PrometheusRemoteWrite{
			{},
		},
	}

	err := c.Validate()
	require.Equal(t, "prometheus-remote-write.path must be set", err.Error())
}

func TestConfig_ValidatePromRemoteWrite_PromRemoteWrite(t *testing.T) {
	c := Config{
		PrometheusRemoteWrite: []*PrometheusRemoteWrite{
			{
				Path:     "/receive",
				Database: "foo",
			},
			{
				Path:     "/receive",
				Database: "foo",
			},
		},
	}

	require.Equal(t, "prometheus-remote-write.path /receive is already defined", c.Validate().Error())
}

func TestConfig_ValidatePromRemoteWrite_EmptyAddLabels(t *testing.T) {
	c := Config{
		PrometheusRemoteWrite: []*PrometheusRemoteWrite{
			{
				Path: "/receive",
				AddLabels: map[string]string{
					"foo": "",
				},
				Database: "foo",
			},
		},
	}

	require.Equal(t, "prometheus-remote-write.add-labels value must be set", c.Validate().Error())

	c = Config{
		PrometheusRemoteWrite: []*PrometheusRemoteWrite{
			{
				Path: "/receive",
				AddLabels: map[string]string{
					"": "bar",
				},
				Database: "foo",
			},
		},
	}

	require.Equal(t, "prometheus-remote-write.add-labels key must be set", c.Validate().Error())
}

func TestConfig_ValidatePromRemoteWrite_EmptyDropLabels(t *testing.T) {
	c := Config{
		PrometheusRemoteWrite: []*PrometheusRemoteWrite{
			{
				Path: "/receive",
				DropLabels: map[string]string{
					"foo": "",
				},
				Database: "foo",
			},
		},
	}

	require.Equal(t, "prometheus-remote-write.drop-labels value must be set", c.Validate().Error())

	c = Config{
		PrometheusRemoteWrite: []*PrometheusRemoteWrite{
			{
				Path: "/receive",
				DropLabels: map[string]string{
					"": "bar",
				},
				Database: "foo",
			},
		},
	}

	require.Equal(t, "prometheus-remote-write.drop-labels key must be set", c.Validate().Error())
}

func TestConfig_ValidateOtelLogs_EmptyAddAttributes(t *testing.T) {
	c := Config{
		OtelLog: &OtelLog{
			AddAttributes: map[string]string{
				"foo": "",
			},
		},
	}

	require.Equal(t, "otel-log.add-attributes value must be set", c.Validate().Error())

	c = Config{
		OtelLog: &OtelLog{
			AddAttributes: map[string]string{
				"": "bar",
			},
		},
	}

	require.Equal(t, "otel-log.add-attributes key must be set", c.Validate().Error())
}

func TestConfig_PromScrape_StaticTargets(t *testing.T) {
	for _, tt := range []struct {
		name    string
		targets []*ScrapeTarget
		err     string
	}{
		{
			name: "empty host regex",
			targets: []*ScrapeTarget{
				{
					HostRegex: "",
				},
			},
			err: "prom-scrape.static-scrape-target[0].host-regex must be set",
		},
		{
			name: "empty url",
			targets: []*ScrapeTarget{
				{
					HostRegex: "foo",
					URL:       "",
				},
			},
			err: "prom-scrape.static-scrape-target[0].url must be set",
		},
		{
			name: "empty namespace",
			targets: []*ScrapeTarget{
				{
					HostRegex: "foo",
					URL:       "http://foo",
				},
			},
			err: "prom-scrape.static-scrape-target[0].namespace must be set",
		},
		{
			name: "empty scheme",
			targets: []*ScrapeTarget{
				{
					HostRegex: "foo",
					URL:       "https://foo",
					Namespace: "foo",
				},
			},
			err: "prom-scrape.static-scrape-target[0].pod must be set",
		},
		{
			name: "empty container",
			targets: []*ScrapeTarget{
				{
					HostRegex: "foo",
					URL:       "https://foo",
					Namespace: "foo",
					Pod:       "foo",
				},
			},
			err: "prom-scrape.static-scrape-target[0].container must be set",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{
				PrometheusScrape: &PrometheusScrape{
					Database:           "foo",
					StaticScrapeTarget: tt.targets,
				},
			}
			require.Equal(t, tt.err, c.Validate().Error())
		})
	}
}

func TestConfig_PromScrape_Interval(t *testing.T) {
	for _, tt := range []struct {
		name     string
		interval int
		err      string
	}{
		{
			name:     "empty interval",
			interval: 0,
			err:      "prom-scrape.scrape-interval must be greater than 0",
		},
		{
			name:     "invalid interval",
			interval: -1,
			err:      "prom-scrape.scrape-interval must be greater than 0",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{
				PrometheusScrape: &PrometheusScrape{
					Database:              "foo",
					ScrapeIntervalSeconds: tt.interval,
				},
			}
			require.Equal(t, tt.err, c.Validate().Error())
		})
	}
}

func TestConfig_PromScrape_Database(t *testing.T) {
	c := Config{
		PrometheusScrape: &PrometheusScrape{
			Database: "",
		},
	}
	require.Equal(t, "prom-scrape.database must be set", c.Validate().Error())
}

func TestConfig_PromWrite_Database(t *testing.T) {
	c := Config{
		PrometheusRemoteWrite: []*PrometheusRemoteWrite{
			{
				Path:     "/receive",
				Database: "",
			},
		},
	}
	require.Equal(t, "prometheus-remote-write.database must be set", c.Validate().Error())
}

func TestConfig_ReplaceVariables(t *testing.T) {
	c := &Config{
		AddLabels: map[string]string{
			"foo": "$(HOSTNAME)_bar",
			"bar": "bar",
		},
		PrometheusRemoteWrite: []*PrometheusRemoteWrite{
			{
				Path:     "/receive",
				Database: "$(HOSTNAME)_bar",
			},
		},
		PrometheusScrape: &PrometheusScrape{
			Database: "$(HOSTNAME)_bar",
			StaticScrapeTarget: []*ScrapeTarget{
				{
					URL: "http://$(HOSTNAME):9999",
				},
			},
		},
	}

	c.ReplaceVariable("$(HOSTNAME)", "FOO")
	require.Equal(t, "FOO_bar", c.PrometheusRemoteWrite[0].Database)
	require.Equal(t, "FOO_bar", c.PrometheusScrape.Database)
	require.Equal(t, "http://FOO:9999", c.PrometheusScrape.StaticScrapeTarget[0].URL)
	require.Equal(t, "FOO_bar", c.AddLabels["foo"])
}
