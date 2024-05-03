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

func TestConfig_ValidateConfig_TailLog(t *testing.T) {
	c := Config{
		TailLog: []*TailLog{
			&TailLog{
				DisableKubeDiscovery: true,
			},
			&TailLog{},
		},
	}

	require.NoError(t, c.Validate())

	// Both taillog configs will try to discover Kubernetes pods.
	c = Config{
		TailLog: []*TailLog{
			&TailLog{},
			&TailLog{},
		},
	}

	require.Equal(t, "tail-log.disable-kube-discovery not set for more than one TailLog configuration", c.Validate().Error())
}

func TestConfig_OtelMetrics(t *testing.T) {
	type testcase struct {
		name   string
		target *OtelMetric
		err    string
	}

	trueVal := true
	overlappingPath := "/metrics"
	testcases := []testcase{
		{
			name: "valid db and config",
			target: &OtelMetric{
				Database:                 "foo",
				Path:                     "/v1/metrics",
				GrpcPort:                 1234,
				DisableMetricsForwarding: &trueVal,
				DefaultDropMetrics:       &trueVal,
				AddLabels: map[string]string{
					"foo": "bar",
				},
				DropLabels: map[string]string{
					"bar": "foo",
				},
				DropMetrics: []string{"badMetric"},
				KeepMetrics: []string{"goodMetric"},
				KeepMetricsWithLabelValue: []LabelMatcher{
					{LabelRegex: "foo", ValueRegex: "bar"},
				},
			},
			err: "",
		},
		{
			name: "Only Path, no GRPC",
			target: &OtelMetric{
				Database: "foo",
				Path:     "/v1/metrics",
			},
			err: "",
		},
		{
			name: "Only GRPC, no Path",
			target: &OtelMetric{
				Database: "foo",
				GrpcPort: 1234,
			},
			err: "",
		},
		{
			name: "No GRPC and no Path",
			target: &OtelMetric{
				Database: "foo",
			},
			err: "otel-metric.path or otel-metric.grpc-port must be set",
		},
		{
			name: "overlapping path",
			target: &OtelMetric{
				Database:                 "foo",
				Path:                     overlappingPath,
				GrpcPort:                 1234,
				DisableMetricsForwarding: &trueVal,
				DefaultDropMetrics:       &trueVal,
				AddLabels: map[string]string{
					"foo": "bar",
				},
				DropLabels: map[string]string{
					"bar": "foo",
				},
				DropMetrics: []string{"badMetric"},
				KeepMetrics: []string{"goodMetric"},
				KeepMetricsWithLabelValue: []LabelMatcher{
					{LabelRegex: "foo", ValueRegex: "bar"},
				},
			},
			err: "otel-metric.path /metrics is already defined",
		},
		{
			name:   "empty db",
			target: &OtelMetric{},
			err:    "otel-metric.database must be set",
		},
		{
			name: "negative grpc port",
			target: &OtelMetric{
				Database: "foo",
				GrpcPort: -1,
			},
			err: "otel-metric.grpc-port must be between 1 and 65535",
		},
		{
			name: "too big grpc port",
			target: &OtelMetric{
				Database: "foo",
				GrpcPort: 65536,
			},
			err: "otel-metric.grpc-port must be between 1 and 65535",
		},
		{
			name: "bad add labels keys",
			target: &OtelMetric{
				Database: "foo",
				Path:     "/v1/metrics",
				AddLabels: map[string]string{
					"": "bar",
				},
			},
			err: "otel-metric.add-labels key must be set",
		},
		{
			name: "bad add labels values",
			target: &OtelMetric{
				Database: "foo",
				Path:     "/v1/metrics",
				AddLabels: map[string]string{
					"foo": "",
				},
			},
			err: "otel-metric.add-labels value must be set",
		},
		{
			name: "bad drop labels keys",
			target: &OtelMetric{
				Database: "foo",
				Path:     "/v1/metrics",
				DropLabels: map[string]string{
					"": "bar",
				},
			},
			err: "otel-metric.drop-labels key must be set",
		},
		{
			name: "bad drop labels values",
			target: &OtelMetric{
				Database: "foo",
				Path:     "/v1/metrics",
				DropLabels: map[string]string{
					"foo": "",
				},
			},
			err: "otel-metric.drop-labels value must be set",
		},
		{
			name: "bad keep metrics with label value keys",
			target: &OtelMetric{
				Database: "foo",
				Path:     "/v1/metrics",
				KeepMetricsWithLabelValue: []LabelMatcher{
					{LabelRegex: "", ValueRegex: "bar"},
				},
			},
			err: "otel-metric.keep-metrics-with-label-value label-regex must be set",
		},
		{
			name: "bad keep metrics with label value value",
			target: &OtelMetric{
				Database: "foo",
				Path:     "/v1/metrics",
				KeepMetricsWithLabelValue: []LabelMatcher{
					{LabelRegex: "foo", ValueRegex: ""},
				},
			},
			err: "otel-metric.keep-metrics-with-label-value value-regex must be set",
		},
	}

	for _, tt := range testcases {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{
				PrometheusRemoteWrite: []*PrometheusRemoteWrite{
					{
						Path:     overlappingPath,
						Database: "foo",
					},
				},
				OtelMetric: []*OtelMetric{tt.target},
			}
			if tt.err != "" {
				require.Error(t, c.Validate())
				require.Equal(t, tt.err, c.Validate().Error())
			} else {
				require.NoError(t, c.Validate())
			}
		})
	}

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

func TestConfig_Validate_PrometheusRemoteWrite(t *testing.T) {
	tests := []struct {
		name    string
		config  PrometheusRemoteWrite
		wantErr bool
	}{
		{
			name: "Success_all_parameters_set",
			config: PrometheusRemoteWrite{
				Database: "db",
				Path:     "/path",
				AddLabels: map[string]string{
					"key1": "value1",
				},
				DropLabels: map[string]string{
					"key2": "value2",
				},
				KeepMetricsWithLabelValue: []LabelMatcher{
					{LabelRegex: "key3", ValueRegex: "value3"},
				},
			},
			wantErr: false,
		},
		{
			name: "Failure_empty_path",
			config: PrometheusRemoteWrite{
				Database: "db",
			},
			wantErr: true,
		},
		{
			name: "Failure_empty_database",
			config: PrometheusRemoteWrite{
				Path: "/path",
			},
			wantErr: true,
		},
		{
			name: "Failure_empty_key_in_AddLabels",
			config: PrometheusRemoteWrite{
				Database: "db",
				Path:     "/path",
				AddLabels: map[string]string{
					"": "value",
				},
			},
			wantErr: true,
		},
		{
			name: "Failure_empty_value_in_AddLabels",
			config: PrometheusRemoteWrite{
				Database: "db",
				Path:     "/path",
				AddLabels: map[string]string{
					"key": "",
				},
			},
			wantErr: true,
		},
		{
			name: "Failure_empty_key_in_DropLabels",
			config: PrometheusRemoteWrite{
				Database: "db",
				Path:     "/path",
				DropLabels: map[string]string{
					"": "value",
				},
			},
			wantErr: true,
		},
		{
			name: "Failure_empty_value_in_DropLabels",
			config: PrometheusRemoteWrite{
				Database: "db",
				Path:     "/path",
				DropLabels: map[string]string{
					"key": "",
				},
			},
			wantErr: true,
		},

		{
			name: "Failure_empty_key_in_KeepMetricsWithLabelValue",
			config: PrometheusRemoteWrite{
				Database: "db",
				Path:     "/path",
				KeepMetricsWithLabelValue: []LabelMatcher{
					{LabelRegex: "", ValueRegex: "value"},
				},
			},
			wantErr: true,
		},
		{
			name: "Failure_empty_value_in_KeepMetricsWithLabelValue",
			config: PrometheusRemoteWrite{
				Database: "db",
				Path:     "/path",
				KeepMetricsWithLabelValue: []LabelMatcher{
					{LabelRegex: "key", ValueRegex: ""},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_Validate_TailLog(t *testing.T) {
	tests := []struct {
		name    string
		config  *TailLog
		wantErr bool
	}{
		{
			name: "Success_valid_static_targets",
			config: &TailLog{
				DisableKubeDiscovery: true,
				AddAttributes:        map[string]string{"key": "value"},
				StaticTailTarget: []*TailTarget{
					{
						FilePath: "/path",
						LogType:  "docker",
						Database: "db",
						Table:    "table",
						Parsers: []string{
							"json",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Success_empty_log_type_default",
			config: &TailLog{
				StaticTailTarget: []*TailTarget{
					{
						FilePath: "/path",
						Database: "db",
						Table:    "table",
						Parsers: []string{
							"json",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Success_no_log_type_no_parsers",
			config: &TailLog{
				StaticTailTarget: []*TailTarget{
					{
						FilePath: "/path",
						Database: "db",
						Table:    "table",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Failure_empty_path",
			config: &TailLog{
				StaticTailTarget: []*TailTarget{
					{
						LogType:  "docker",
						Database: "db",
						Table:    "table",
						Parsers: []string{
							"json",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Failure_empty_db",
			config: &TailLog{
				StaticTailTarget: []*TailTarget{
					{
						FilePath: "/path",
						LogType:  "docker",
						Table:    "table",
						Parsers: []string{
							"json",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Failure_empty_table",
			config: &TailLog{
				StaticTailTarget: []*TailTarget{
					{
						FilePath: "/path",
						LogType:  "docker",
						Database: "db",
						Parsers: []string{
							"json",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Failure invalid parser",
			config: &TailLog{
				StaticTailTarget: []*TailTarget{
					{
						FilePath: "/path",
						LogType:  "docker",
						Database: "db",
						Table:    "table",
						Parsers: []string{
							"json",
							"invalid",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Failure_invalid_log_type",
			config: &TailLog{
				StaticTailTarget: []*TailTarget{
					{
						FilePath: "/path",
						LogType:  "foo",
						Database: "db",
						Table:    "table",
						Parsers: []string{
							"json",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Failure_empty_attribute_key",
			config: &TailLog{
				AddAttributes: map[string]string{"": "value"},
				StaticTailTarget: []*TailTarget{
					{
						FilePath: "/path",
						Database: "db",
						Table:    "table",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Failure_empty_attribute_value",
			config: &TailLog{
				AddAttributes: map[string]string{"key": ""},
				StaticTailTarget: []*TailTarget{
					{
						FilePath: "/path",
						Database: "db",
						Table:    "table",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Success_valid_transform",
			config: &TailLog{
				StaticTailTarget: []*TailTarget{
					{
						FilePath: "/path",
						Database: "db",
						Table:    "table",
					},
				},
				Transforms: []*TailTransform{
					{
						Name: "plugin",
						Config: map[string]interface{}{
							"GoPath":     "path",
							"ImportName": "import",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Failure_invalid_transform",
			config: &TailLog{
				StaticTailTarget: []*TailTarget{
					{
						FilePath: "/path",
						Database: "db",
						Table:    "table",
					},
				},
				Transforms: []*TailTransform{
					{
						Name: "plugin",
						Config: map[string]interface{}{
							"GoPath":     "path",
							"ImportName": "import",
						},
					},
					{
						Name:   "shootlasers",
						Config: map[string]interface{}{},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
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

func TestConfig_TLS(t *testing.T) {
	type testcase struct {
		name    string
		config  Config
		wantErr bool
	}

	testcases := []testcase{
		{
			name: "Both TLS fields set",
			config: Config{
				TLSCertFile: "cert",
				TLSKeyFile:  "key",
			},
			wantErr: false,
		},
		{
			name:    "No TLS fields set",
			config:  Config{},
			wantErr: false,
		},
		{
			name: "Cert Only",
			config: Config{
				TLSCertFile: "cert",
			},
			wantErr: true,
		},
		{
			name: "Key Only",
			config: Config{
				TLSKeyFile: "key",
			},
			wantErr: true,
		},
	}

	for _, tt := range testcases {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
