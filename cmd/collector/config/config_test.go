package config

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

func TestConfig_Validate_InvalidStorageBackend(t *testing.T) {
	c := Config{StorageBackend: "invalid"}
	err := c.Validate()
	require.EqualError(t, err, `unknown storage backend "invalid"`)
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

func TestConfig_Validate_MetadataWatch(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "metadata watch omitted",
			cfg:  Config{},
		},
		{
			name: "metadata watch configured",
			cfg: Config{
				MetadataWatch: &MetadataWatch{},
			},
		},
		{
			name: "metadata watch kubernetes node",
			cfg: Config{
				MetadataWatch: &MetadataWatch{KubernetesNode: &MetadataWatchKubernetesNode{}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestConfig_Validate_AddMetadataLabels(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "add metadata labels omitted",
			cfg:  Config{},
		},
		{
			name: "kubernetes node metadata not requested",
			cfg: Config{
				AddMetadataLabels: &AddMetadataLabels{},
			},
		},
		{
			name: "metadata labels require watcher",
			cfg: Config{
				AddMetadataLabels: &AddMetadataLabels{
					KubernetesNode: &AddMetadataKubernetesNode{},
				},
			},
			wantErr: "metadata-watch.kubernetes-node must be configured when add-metadata-labels.kubernetes-node is used",
		},
		{
			name: "invalid kubernetes label key",
			cfg: Config{
				MetadataWatch: &MetadataWatch{KubernetesNode: &MetadataWatchKubernetesNode{}},
				AddMetadataLabels: &AddMetadataLabels{
					KubernetesNode: &AddMetadataKubernetesNode{
						Labels: map[string]string{
							"": "dest",
						},
					},
				},
			},
			wantErr: "add-metadata-labels.kubernetes-node.labels key must be set",
		},
		{
			name: "metadata labels success",
			cfg: Config{
				MetadataWatch: &MetadataWatch{KubernetesNode: &MetadataWatchKubernetesNode{}},
				AddMetadataLabels: &AddMetadataLabels{
					KubernetesNode: &AddMetadataKubernetesNode{
						Labels: map[string]string{
							"kubernetes.io/role": "node_role",
						},
						Annotations: map[string]string{
							"cluster-autoscaler.kubernetes.io/safe-to-evict": "safe_to_evict",
						},
					},
				},
			},
		},
		{
			name: "prometheus scrape metadata labels require watcher",
			cfg: Config{
				PrometheusScrape: &PrometheusScrape{
					Database:              "metrics",
					StaticScrapeTarget:    []*ScrapeTarget{},
					ScrapeIntervalSeconds: 30,
					ScrapeTimeout:         30,
					AddMetadataLabels: &AddMetadataLabels{
						KubernetesNode: &AddMetadataKubernetesNode{},
					},
				},
			},
			wantErr: "prometheus-scrape.add-metadata-labels: metadata-watch.kubernetes-node must be configured when add-metadata-labels.kubernetes-node is used",
		},
		{
			name: "prometheus scrape metadata labels success",
			cfg: Config{
				MetadataWatch: &MetadataWatch{KubernetesNode: &MetadataWatchKubernetesNode{}},
				PrometheusScrape: &PrometheusScrape{
					Database:              "metrics",
					StaticScrapeTarget:    []*ScrapeTarget{},
					ScrapeIntervalSeconds: 30,
					ScrapeTimeout:         30,
					AddMetadataLabels: &AddMetadataLabels{
						KubernetesNode: &AddMetadataKubernetesNode{
							Labels: map[string]string{"role": "node_role"},
						},
					},
				},
			},
		},
		{
			name: "prometheus remote write metadata labels require watcher",
			cfg: Config{
				PrometheusRemoteWrite: []*PrometheusRemoteWrite{
					{
						Path:     "/receive",
						Database: "metrics",
						AddMetadataLabels: &AddMetadataLabels{
							KubernetesNode: &AddMetadataKubernetesNode{},
						},
					},
				},
			},
			wantErr: "prometheus-remote-write[/receive].add-metadata-labels: metadata-watch.kubernetes-node must be configured when add-metadata-labels.kubernetes-node is used",
		},
		{
			name: "prometheus remote write metadata labels success",
			cfg: Config{
				MetadataWatch: &MetadataWatch{KubernetesNode: &MetadataWatchKubernetesNode{}},
				PrometheusRemoteWrite: []*PrometheusRemoteWrite{
					{
						Path:     "/receive",
						Database: "metrics",
						AddMetadataLabels: &AddMetadataLabels{
							KubernetesNode: &AddMetadataKubernetesNode{
								Labels: map[string]string{"role": "node_role"},
							},
						},
					},
				},
			},
		},
		{
			name: "otel metric metadata labels require watcher",
			cfg: Config{
				OtelMetric: []*OtelMetric{
					{
						Path:     "/v1/metrics",
						Database: "metrics",
						AddMetadataLabels: &AddMetadataLabels{
							KubernetesNode: &AddMetadataKubernetesNode{},
						},
					},
				},
			},
			wantErr: "otel-metric[0].add-metadata-labels: metadata-watch.kubernetes-node must be configured when add-metadata-labels.kubernetes-node is used",
		},
		{
			name: "otel metric metadata labels success",
			cfg: Config{
				MetadataWatch: &MetadataWatch{KubernetesNode: &MetadataWatchKubernetesNode{}},
				OtelMetric: []*OtelMetric{
					{
						Path:     "/v1/metrics",
						Database: "metrics",
						AddMetadataLabels: &AddMetadataLabels{
							KubernetesNode: &AddMetadataKubernetesNode{
								Labels: map[string]string{"role": "node_role"},
							},
						},
					},
				},
			},
		},
		{
			name: "otel log metadata labels require watcher",
			cfg: Config{
				OtelLog: &OtelLog{
					AddMetadataLabels: &AddMetadataLabels{
						KubernetesNode: &AddMetadataKubernetesNode{},
					},
				},
			},
			wantErr: "otel-log.add-metadata-labels: metadata-watch.kubernetes-node must be configured when add-metadata-labels.kubernetes-node is used",
		},
		{
			name: "otel log metadata labels success",
			cfg: Config{
				MetadataWatch: &MetadataWatch{KubernetesNode: &MetadataWatchKubernetesNode{}},
				OtelLog: &OtelLog{
					AddMetadataLabels: &AddMetadataLabels{
						KubernetesNode: &AddMetadataKubernetesNode{
							Labels: map[string]string{"role": "node_role"},
						},
					},
				},
			},
		},
		{
			name: "host log metadata labels require watcher",
			cfg: Config{
				HostLog: []*HostLog{
					{
						AddMetadataLabels: &AddMetadataLabels{
							KubernetesNode: &AddMetadataKubernetesNode{},
						},
					},
				},
			},
			wantErr: "host-log[0].add-metadata-labels: metadata-watch.kubernetes-node must be configured when add-metadata-labels.kubernetes-node is used",
		},
		{
			name: "host log metadata labels success",
			cfg: Config{
				MetadataWatch: &MetadataWatch{KubernetesNode: &MetadataWatchKubernetesNode{}},
				HostLog: []*HostLog{
					{
						AddMetadataLabels: &AddMetadataLabels{
							KubernetesNode: &AddMetadataKubernetesNode{
								Labels: map[string]string{"role": "node_role"},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
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

func TestConfig_ValidatePromRemoteWrite_Exporters(t *testing.T) {
	type testcase struct {
		name   string
		config Config
		err    string
	}

	testcases := []testcase{
		{
			name: "Success",
			config: Config{
				PrometheusRemoteWrite: []*PrometheusRemoteWrite{
					{
						Path:     "/receive",
						Database: "foo",
						Exporters: []string{
							"foo",
							"bar",
						},
					},
				},
				Exporters: &Exporters{
					OtlpMetricExport: []*OtlpMetricExport{
						{
							Name:        "foo",
							Destination: "http://foo",
						},
						{
							Name:        "bar",
							Destination: "http://bar",
						},
					},
				},
			},
		},
		{
			name: "Missing exporter",
			config: Config{
				PrometheusRemoteWrite: []*PrometheusRemoteWrite{
					{
						Path:     "/receive",
						Database: "foo",
						Exporters: []string{
							"foo",
							"bar",
						},
					},
				},
				Exporters: &Exporters{
					OtlpMetricExport: []*OtlpMetricExport{
						{
							Name:        "foo",
							Destination: "http://foo",
						},
					},
				},
			},
			err: `prometheus-remote-write.exporters "bar" not found`,
		},
	}

	for _, tt := range testcases {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.err != "" {
				require.Error(t, err)
				require.Equal(t, tt.err, err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
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

func TestConfig_ValidateConfig_OtelLog_Exporters(t *testing.T) {
	type testcase struct {
		name   string
		config Config
		err    string
	}

	testcases := []testcase{
		{
			name: "Success",
			config: Config{
				OtelLog: &OtelLog{
					Exporters: []string{
						"foo",
						"bar",
					},
				},
				Exporters: &Exporters{
					FluentForwardLogExport: []*FluentForwardLogExport{
						{
							Name:         "foo",
							Destination:  "unix:///tmp/fluent.sock",
							TagAttribute: "foo_log_tag",
						},
						{
							Name:         "bar",
							Destination:  "unix:///tmp/fluent2.sock",
							TagAttribute: "bar_log_tag",
						},
					},
				},
			},
		},
		{
			name: "Missing exporter",
			config: Config{
				OtelLog: &OtelLog{
					Exporters: []string{
						"foo",
						"bar",
					},
				},
				Exporters: &Exporters{
					FluentForwardLogExport: []*FluentForwardLogExport{
						{
							Name:         "foo",
							Destination:  "unix:///tmp/fluent.sock",
							TagAttribute: "foo_log_tag",
						},
					},
				},
			},
			err: `otel-log.exporters "bar" not found`,
		},
	}

	for _, tt := range testcases {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.err != "" {
				require.Error(t, err)
				require.Equal(t, tt.err, err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfig_ValidateOtelLog_Transforms(t *testing.T) {
	tests := []struct {
		name    string
		otelLog *OtelLog
		wantErr bool
	}{
		{
			name: "Success_valid_transform",
			otelLog: &OtelLog{
				Transforms: []*LogTransform{
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
			otelLog: &OtelLog{
				Transforms: []*LogTransform{
					{
						Name: "plugin",
						Config: map[string]interface{}{
							"GoPath":     "path",
							"ImportName": "import",
						},
					},
					{
						Name:   "unknown-type",
						Config: map[string]interface{}{},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{
				OtelLog: tt.otelLog,
			}
			err := c.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfig_ValidateConfig_HostLog(t *testing.T) {
	type testcase struct {
		name   string
		config Config
		err    string
	}

	testcases := []testcase{
		{
			name: "Success - single host log with kube discovery enabled",
			config: Config{
				HostLog: []*HostLog{
					{
						DisableKubeDiscovery: true,
					},
					{},
				},
			},
		},
		{
			name: "Failure - multiple host logs with kube discovery enabled",
			config: Config{
				HostLog: []*HostLog{
					{},
					{},
				},
			},
			err: "host-log[1].disable-kube-discovery not set for more than one HostLog configuration",
		},
		{
			name: "Success - valid exporters",
			config: Config{
				HostLog: []*HostLog{
					{
						DisableKubeDiscovery: true,
						Exporters: []string{
							"foo",
						},
					},
					{
						Exporters: []string{
							"bar",
						},
					},
				},
				Exporters: &Exporters{
					FluentForwardLogExport: []*FluentForwardLogExport{
						{
							Name:         "foo",
							Destination:  "unix:///tmp/fluent.sock",
							TagAttribute: "foo_log_tag",
						},
						{
							Name:         "bar",
							Destination:  "unix:///tmp/fluent2.sock",
							TagAttribute: "bar_log_tag",
						},
					},
				},
			},
		},
		{
			name: "Failure - missing exporter",
			config: Config{
				HostLog: []*HostLog{
					{
						DisableKubeDiscovery: true,
						Exporters: []string{
							"foo",
						},
					},
					{
						Exporters: []string{
							"bar",
						},
					},
				},
				Exporters: &Exporters{
					FluentForwardLogExport: []*FluentForwardLogExport{
						{
							Name:         "foo",
							Destination:  "unix:///tmp/fluent.sock",
							TagAttribute: "foo_log_tag",
						},
					},
				},
			},
			err: `host-log[1].exporters "bar" not found`,
		},
	}

	for _, tt := range testcases {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.err != "" {
				require.Error(t, err)
				require.Equal(t, tt.err, err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
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
			err: "otel-metric[0].path /metrics is already defined",
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

func TestConfig_OtelMetrics_Exporters(t *testing.T) {
	type testcase struct {
		name   string
		config Config
		err    string
	}

	tests := []testcase{
		{
			name: "Success",
			config: Config{
				OtelMetric: []*OtelMetric{
					{
						Database: "foo",
						Path:     "/v1/metrics",
						GrpcPort: 1234,
						Exporters: []string{
							"foo",
							"bar",
						},
					},
				},
				Exporters: &Exporters{
					OtlpMetricExport: []*OtlpMetricExport{
						{
							Name:        "foo",
							Destination: "http://foo",
						},
						{
							Name:        "bar",
							Destination: "http://bar",
						},
					},
				},
			},
		},
		{
			name: "Missing exporter",
			config: Config{
				OtelMetric: []*OtelMetric{
					{
						Database: "foo",
						Path:     "/v1/metrics",
						GrpcPort: 1234,
						Exporters: []string{
							"foo",
							"bar",
						},
					},
				},
				Exporters: &Exporters{
					OtlpMetricExport: []*OtlpMetricExport{
						{
							Name:        "foo",
							Destination: "http://foo",
						},
					},
				},
			},
			err: `otel-metric[0].exporters "bar" not found`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.err != "" {
				require.Error(t, err)
				require.Equal(t, tt.err, err.Error())
			} else {
				require.NoError(t, err)
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

func TestConfig_PromScape_Exporters(t *testing.T) {
	type testcase struct {
		name   string
		config Config
		err    string
	}

	tests := []testcase{
		{
			name: "Success",
			config: Config{
				PrometheusScrape: &PrometheusScrape{
					Database:              "foo",
					ScrapeIntervalSeconds: 10,
					Exporters: []string{
						"foo",
						"bar",
					},
				},

				Exporters: &Exporters{
					OtlpMetricExport: []*OtlpMetricExport{
						{
							Name:        "foo",
							Destination: "http://foo",
						},
						{
							Name:        "bar",
							Destination: "http://bar",
						},
					},
				},
			},
			err: "",
		},
		{
			name: "Missing exporter",
			config: Config{
				PrometheusScrape: &PrometheusScrape{
					Database:              "foo",
					ScrapeIntervalSeconds: 10,
					Exporters: []string{
						"foo",
						"bar",
					},
				},

				Exporters: &Exporters{
					OtlpMetricExport: []*OtlpMetricExport{
						{
							Name:        "foo",
							Destination: "http://foo",
						},
					},
				},
			},
			err: `prometheus-scrape.exporters "bar" not found`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.err != "" {
				require.Error(t, err)
				require.Equal(t, tt.err, err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
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

func TestConfig_Validate_HostLog(t *testing.T) {
	tests := []struct {
		name    string
		config  *HostLog
		wantErr bool
	}{
		{
			name: "Success_valid_static_targets",
			config: &HostLog{
				DisableKubeDiscovery: true,
				AddAttributes:        map[string]string{"key": "value"},
				StaticFileTargets: []*TailTarget{
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
			config: &HostLog{
				StaticFileTargets: []*TailTarget{
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
			config: &HostLog{
				StaticFileTargets: []*TailTarget{
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
			config: &HostLog{
				StaticFileTargets: []*TailTarget{
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
			config: &HostLog{
				StaticFileTargets: []*TailTarget{
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
			config: &HostLog{
				StaticFileTargets: []*TailTarget{
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
			config: &HostLog{
				StaticFileTargets: []*TailTarget{
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
			name: "Failure invalid log type",
			config: &HostLog{
				StaticFileTargets: []*TailTarget{
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
			name: "Failure empty attribute key",
			config: &HostLog{
				AddAttributes: map[string]string{"": "value"},
				StaticFileTargets: []*TailTarget{
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
			name: "Failure empty attribute value",
			config: &HostLog{
				AddAttributes: map[string]string{"key": ""},
				StaticFileTargets: []*TailTarget{
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
			config: &HostLog{
				StaticFileTargets: []*TailTarget{
					{
						FilePath: "/path",
						Database: "db",
						Table:    "table",
					},
				},
				Transforms: []*LogTransform{
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
			config: &HostLog{
				StaticFileTargets: []*TailTarget{
					{
						FilePath: "/path",
						Database: "db",
						Table:    "table",
					},
				},
				Transforms: []*LogTransform{
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
		{
			name: "Failure invalid journald target",
			config: &HostLog{
				JournalTargets: []*JournalTarget{
					{
						Database: "db",
						// Expects table
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

func TestConfig_JournalTarget_Validate(t *testing.T) {
	tests := []struct {
		name   string
		config *JournalTarget
		err    string
	}{
		{
			name: "Success_valid_journal_target",
			config: &JournalTarget{
				Database: "db",
				Table:    "table",
			},
			err: "",
		},
		{
			name: "Failure_empty_database",
			config: &JournalTarget{
				Table: "table",
			},
			err: "database must be set",
		},
		{
			name: "Failure_empty_table",
			config: &JournalTarget{
				Database: "db",
			},
			err: "table must be set",
		},
		{
			name: "Success_valid_journal_target_with_filters",
			config: &JournalTarget{
				Database: "db",
				Table:    "table",
				Matches: []string{
					"_SYSTEMD_UNIT=foo",
					"_PID = 2021",
				},
			},
			err: "",
		},
		{
			name: "Success_valid_journal_target_with_filters_and_disjunction",
			config: &JournalTarget{
				Database: "db",
				Table:    "table",
				Matches: []string{
					"_SYSTEMD_UNIT=foo",
					"_SYSTEMD_UNIT=bar",
					"+",
					"_PID = 2021",
				},
			},
			err: "",
		},
		{
			name: "Failure_filter_without_value",
			config: &JournalTarget{
				Database: "db",
				Table:    "table",
				Matches: []string{
					"",
					"_PID=2021",
				},
			},
			err: "match must have a value",
		},
		{
			name: "Failure_filter_non_kv",
			config: &JournalTarget{
				Database: "db",
				Table:    "table",
				Matches: []string{
					"_PID",
				},
			},
			err: "match _PID must be in the format key=value",
		},
		{
			name: "Failure_filter_only_key",
			config: &JournalTarget{
				Database: "db",
				Table:    "table",
				Matches: []string{
					"_PID=",
				},
			},
			err: "match _PID= must have a value",
		},
		{
			name: "Failure_filter_only_value",
			config: &JournalTarget{
				Database: "db",
				Table:    "table",
				Matches: []string{
					"=2021",
				},
			},
			err: "match =2021 must have a key",
		},
		{
			name: "Failure_filter_initial_disjunction",
			config: &JournalTarget{
				Database: "db",
				Table:    "table",
				Matches: []string{
					"+",
					"_PID=2021",
				},
			},
			err: "matches must not start with +",
		},
		{
			name: "Success_valid_journal_target_with_parser",
			config: &JournalTarget{
				Database: "db",
				Table:    "table",
				Parsers:  []string{"json"},
			},
			err: "",
		},
		{
			name: "Failure_invalid_parser",
			config: &JournalTarget{
				Database: "db",
				Table:    "table",
				Parsers:  []string{"json", "does-not-exist"},
			},
			err: "parsers does-not-exist is not a valid parser",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.err != "" {
				require.Error(t, err)
				require.Equal(t, tt.err, err.Error())
			} else {
				require.NoError(t, err)
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

func TestConfig_ValidateLiftLabels(t *testing.T) {
	c := Config{
		LiftLabels: []*LiftLabel{
			{
				Name:       "x",
				ColumnName: "x:1",
			},
		},
	}

	require.Equal(t, "lift-labels.column-name `x:1` contains invalid characters", c.Validate().Error())

	c = Config{
		LiftLabels: []*LiftLabel{
			{
				Name: "x",
			},
		}}
	require.NoError(t, c.Validate())

	c = Config{
		LiftLabels: []*LiftLabel{
			{
				Name:       "x",
				ColumnName: "y",
			},
		}}
	require.NoError(t, c.Validate())
}

func TestConfig_WALFlushInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval int
		wantErr  bool
	}{
		{
			name:     "WALFlushIntervalGreaterThanZero",
			interval: 100,
			wantErr:  false,
		},
		{
			name:     "WALFlushIntervalZero",
			interval: 100,
			wantErr:  false,
		},
		{
			name:     "WALFlushIntervalNegative",
			interval: -100,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{
				WALFlushIntervalMilliSeconds: tt.interval,
			}
			err := c.Validate()
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, "wal-flush-interval must be greater than 0", err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfig_ValidateExporters(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "Success",
			config: Config{
				Exporters: &Exporters{
					OtlpMetricExport: []*OtlpMetricExport{
						{
							Name:        "foo",
							Destination: "http://foo",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Failure_empty_name",
			config: Config{
				Exporters: &Exporters{
					OtlpMetricExport: []*OtlpMetricExport{
						{
							Destination: "http://foo",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Failure_empty_destination",
			config: Config{
				Exporters: &Exporters{
					OtlpMetricExport: []*OtlpMetricExport{
						{
							Name: "foo",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Failure_duplicate_name",
			config: Config{
				Exporters: &Exporters{
					OtlpMetricExport: []*OtlpMetricExport{
						{
							Name:        "foo",
							Destination: "http://foo",
						},
						{
							Name:        "foo",
							Destination: "http://bar",
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
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

func TestKubeletDiscovery_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  KubeletDiscovery
		wantErr string
	}{
		{
			name:    "valid config with defaults",
			config:  KubeletDiscovery{},
			wantErr: "",
		},
		{
			name: "valid config with all fields set",
			config: KubeletDiscovery{
				Host:           "127.0.0.1",
				Port:           10250,
				PollInterval:   15,
				RequestTimeout: 10,
				TokenPath:      "/var/run/secrets/kubernetes.io/serviceaccount/token",
				CAPath:         "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
			},
			wantErr: "",
		},
		{
			name: "valid config with custom values",
			config: KubeletDiscovery{
				Host:           "10.0.0.1",
				Port:           10255,
				PollInterval:   30,
				RequestTimeout: 20,
				TokenPath:      "/custom/token",
				CAPath:         "/custom/ca.crt",
			},
			wantErr: "",
		},
		{
			name: "invalid port - negative",
			config: KubeletDiscovery{
				Port: -1,
			},
			wantErr: "kubelet-discovery.port must be between 0 and 65535",
		},
		{
			name: "invalid port - too large",
			config: KubeletDiscovery{
				Port: 65536,
			},
			wantErr: "kubelet-discovery.port must be between 0 and 65535",
		},
		{
			name: "invalid poll interval - less than 1",
			config: KubeletDiscovery{
				PollInterval: -1,
			},
			wantErr: "kubelet-discovery.poll-interval must be at least 1 second",
		},
		{
			name: "invalid request timeout - less than 1",
			config: KubeletDiscovery{
				RequestTimeout: -1,
			},
			wantErr: "kubelet-discovery.request-timeout must be at least 1 second",
		},
		{
			name: "valid edge case - port 0",
			config: KubeletDiscovery{
				Port: 0,
			},
			wantErr: "",
		},
		{
			name: "valid edge case - port 65535",
			config: KubeletDiscovery{
				Port: 65535,
			},
			wantErr: "",
		},
		{
			name: "valid edge case - zero timeouts (use defaults)",
			config: KubeletDiscovery{
				PollInterval:   0,
				RequestTimeout: 0,
			},
			wantErr: "",
		},
		{
			name: "valid edge case - minimum valid timeouts",
			config: KubeletDiscovery{
				PollInterval:   1,
				RequestTimeout: 1,
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfig_Validate_KubeletDiscovery(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "kubelet discovery omitted",
			cfg:  Config{},
		},
		{
			name: "kubelet discovery with defaults",
			cfg: Config{
				KubeletDiscovery: &KubeletDiscovery{},
			},
		},
		{
			name: "kubelet discovery with valid config",
			cfg: Config{
				KubeletDiscovery: &KubeletDiscovery{
					Host:           "127.0.0.1",
					Port:           10250,
					PollInterval:   15,
					RequestTimeout: 10,
					TokenPath:      "/var/run/secrets/kubernetes.io/serviceaccount/token",
					CAPath:         "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
				},
			},
		},
		{
			name: "kubelet discovery with invalid port",
			cfg: Config{
				KubeletDiscovery: &KubeletDiscovery{
					Port: -1,
				},
			},
			wantErr: "kubelet-discovery.port must be between 0 and 65535",
		},
		{
			name: "kubelet discovery with invalid poll interval",
			cfg: Config{
				KubeletDiscovery: &KubeletDiscovery{
					PollInterval: -5,
				},
			},
			wantErr: "kubelet-discovery.poll-interval must be at least 1 second",
		},
		{
			name: "kubelet discovery with invalid request timeout",
			cfg: Config{
				KubeletDiscovery: &KubeletDiscovery{
					RequestTimeout: -10,
				},
			},
			wantErr: "kubelet-discovery.request-timeout must be at least 1 second",
		},
		{
			name: "kubelet discovery with custom paths",
			cfg: Config{
				KubeletDiscovery: &KubeletDiscovery{
					TokenPath: "/custom/path/token",
					CAPath:    "/custom/path/ca.crt",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
