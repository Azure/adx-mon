package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExporters_Validate(t *testing.T) {
	type testcases struct {
		name        string
		input       Exporters
		expectedErr string
	}

	cases := []testcases{
		{
			name: "valid",
			input: Exporters{
				OtlpMetricExport: []*OtlpMetricExport{
					{
						Name:        "name",
						Destination: "https://localhost:9393",
					},
				},
				FluentForwardLogExport: []*FluentForwardLogExport{
					{
						Name:         "fluent-forward-name",
						Destination:  "tcp://localhost:24224",
						TagAttribute: "log_tag",
					},
				},
			},
			expectedErr: "",
		},
		{
			name: "invalid missing name for exporter",
			input: Exporters{
				OtlpMetricExport: []*OtlpMetricExport{
					{
						Name:        "name",
						Destination: "https://localhost:9393",
					},
					{
						Name:        "",
						Destination: "https://localhost:9392",
					},
				},
			},
			expectedErr: "exporter.otlp-metric-export[1].name must be set",
		},
		{
			name: "duplicate name for exporter",
			input: Exporters{
				OtlpMetricExport: []*OtlpMetricExport{
					{
						Name:        "name",
						Destination: "https://localhost:9393",
					},
				},
				FluentForwardLogExport: []*FluentForwardLogExport{
					{
						Name:         "name",
						Destination:  "tcp://localhost:24224",
						TagAttribute: "log_tag",
					},
				},
			},
			expectedErr: "exporter.fluent-forward-log-export[0].name \"name\" is not unique",
		},
		{
			name: "invalid missing name for fluent exporter",
			input: Exporters{
				OtlpMetricExport: []*OtlpMetricExport{
					{
						Name:        "name",
						Destination: "https://localhost:9393",
					},
				},
				FluentForwardLogExport: []*FluentForwardLogExport{
					{
						Name:         "",
						Destination:  "tcp://localhost:24224",
						TagAttribute: "log_tag",
					},
				},
			},
			expectedErr: "exporter.fluent-forward-log-export[0].name must be set",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.input.Validate()
			if c.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, c.expectedErr, err.Error())
			}
		})
	}
}

func TestOtlpMetricExport_Validate(t *testing.T) {
	type testcases struct {
		name        string
		input       OtlpMetricExport
		expectedErr string
	}
	trueVal := true

	cases := []testcases{
		{
			name: "valid",
			input: OtlpMetricExport{
				Name:               "name",
				Destination:        "https://localhost:9393",
				DefaultDropMetrics: &trueVal,
				DropLabels: map[string]string{
					"metricone": "badlabel",
				},
				DropMetrics: []string{"dropmetric"},
				KeepMetrics: []string{"keepmetric"},
				KeepMetricsWithLabelValue: []LabelMatcher{
					{
						LabelRegex: "label",
						ValueRegex: "value",
					},
				},
			},
		},
		{
			name: "invalid missing name",
			input: OtlpMetricExport{
				Name:        "",
				Destination: "https://localhost:9393",
			},
			expectedErr: "name must be set",
		},
		{
			name: "invalid missing destination",
			input: OtlpMetricExport{
				Name:        "name",
				Destination: "",
			},
			expectedErr: "destination must be set",
		},
		{
			name: "invalid drop labels",
			input: OtlpMetricExport{
				Name:        "name",
				Destination: "https://localhost:9393",
				DropLabels: map[string]string{
					"regex": "label[",
				},
			},
			expectedErr: "invalid exporter config: invalid label regex label[: error parsing regexp: missing closing ]: `[`",
		},
		{
			name: "invalid drop metrics",
			input: OtlpMetricExport{
				Name:        "name",
				Destination: "https://localhost:9393",
				DropMetrics: []string{"label["},
			},
			expectedErr: "invalid exporter config: invalid metric regex label[: error parsing regexp: missing closing ]: `[`",
		},
		{
			name: "invalid keep metrics",
			input: OtlpMetricExport{
				Name:        "name",
				Destination: "https://localhost:9393",
				KeepMetrics: []string{"label["},
			},
			expectedErr: "invalid exporter config: invalid metric regex label[: error parsing regexp: missing closing ]: `[`",
		},
		{
			name: "invalid keep metrics with label value",
			input: OtlpMetricExport{
				Name:        "name",
				Destination: "https://localhost:9393",
				KeepMetricsWithLabelValue: []LabelMatcher{
					{
						LabelRegex: "label[",
						ValueRegex: "value",
					},
				},
			},
			expectedErr: "invalid exporter config: invalid metric regex label[: error parsing regexp: missing closing ]: `[`",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.input.Validate()
			if c.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, c.expectedErr, err.Error())
			}
		})
	}
}

func TestFluentForwardLogExport_Validate(t *testing.T) {
	tests := []struct {
		name        string
		input       FluentForwardLogExport
		expectedErr string
	}{
		{
			name: "valid config",
			input: FluentForwardLogExport{
				Name:         "test-exporter",
				Destination:  "tcp://localhost:24224",
				TagAttribute: "log_tag",
			},
			expectedErr: "",
		},
		{
			name: "missing name",
			input: FluentForwardLogExport{
				Destination:  "tcp://localhost:24224",
				TagAttribute: "log_tag",
			},
			expectedErr: "name must be set",
		},
		{
			name: "missing destination",
			input: FluentForwardLogExport{
				Name:         "test-exporter",
				TagAttribute: "log_tag",
			},
			expectedErr: "destination must be set",
		},
		{
			name: "missing tag attribute",
			input: FluentForwardLogExport{
				Name:        "test-exporter",
				Destination: "tcp://localhost:24224",
			},
			expectedErr: "tag-attribute must be set",
		},
		{
			name: "unix socket destination",
			input: FluentForwardLogExport{
				Name:         "test-exporter",
				Destination:  "unix:///var/run/fluent.sock",
				TagAttribute: "log_tag",
			},
			expectedErr: "",
		},
		{
			name: "no host",
			input: FluentForwardLogExport{
				Name:         "test-exporter",
				Destination:  "tcp://:24224",
				TagAttribute: "log_tag",
			},
			expectedErr: "",
		},
		{
			name: "invalid tcp destination - no port",
			input: FluentForwardLogExport{
				Name:         "test-exporter",
				Destination:  "tcp://localhost",
				TagAttribute: "log_tag",
			},
			expectedErr: "invalid destination tcp://localhost: address localhost: missing port in address",
		},
		{
			name: "invalid tcp destination - invalid port",
			input: FluentForwardLogExport{
				Name:         "test-exporter",
				Destination:  "tcp://localhost:invalid",
				TagAttribute: "log_tag",
			},
			expectedErr: "invalid destination tcp://localhost:invalid: unable to parse invalid as integer",
		},
		{
			name: "invalid tcp destination - too low of port",
			input: FluentForwardLogExport{
				Name:         "test-exporter",
				Destination:  "tcp://localhost:0",
				TagAttribute: "log_tag",
			},
			expectedErr: "invalid destination tcp://localhost:0: port 0 not between 1 and 65535",
		},
		{
			name: "invalid tcp destination - too high of port",
			input: FluentForwardLogExport{
				Name:         "test-exporter",
				Destination:  "tcp://localhost:65536",
				TagAttribute: "log_tag",
			},
			expectedErr: "invalid destination tcp://localhost:65536: port 65536 not between 1 and 65535",
		},
		{
			name: "invalid destination - wrong prefix",
			input: FluentForwardLogExport{
				Name:         "test-exporter",
				Destination:  "file:///var/run/fluent.sock",
				TagAttribute: "log_tag",
			},
			expectedErr: "invalid destination file:///var/run/fluent.sock: must be in the form tcp://<host>:<port> or unix:///path/to/socket",
		},
		{
			name: "invalid destination - no prefix",
			input: FluentForwardLogExport{
				Name:         "test-exporter",
				Destination:  "localhost",
				TagAttribute: "log_tag",
			},
			expectedErr: "invalid destination localhost: must be in the form tcp://<host>:<port> or unix:///path/to/socket",
		},
		{
			name: "invalid unix socket - missing path",
			input: FluentForwardLogExport{
				Name:         "test-exporter",
				Destination:  "unix://",
				TagAttribute: "log_tag",
			},
			expectedErr: "invalid destination unix://: unix socket path is empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, tt.expectedErr, err.Error())
			}
		})
	}
}

func TestGetMetricsExporter(t *testing.T) {
	tests := []struct {
		name         string
		exporters    *Exporters
		exporterName string
		wantErr      bool
		errMsg       string
	}{
		{
			name: "existing exporter",
			exporters: &Exporters{
				OtlpMetricExport: []*OtlpMetricExport{
					{
						Name:        "test-exporter",
						Destination: "https://localhost:9393",
					},
				},
			},
			exporterName: "test-exporter",
			wantErr:      false,
		},
		{
			name:         "nil exporters",
			exporters:    nil,
			exporterName: "test-exporter",
			wantErr:      true,
			errMsg:       "exporters config not set",
		},
		{
			name: "non-existing exporter",
			exporters: &Exporters{
				OtlpMetricExport: []*OtlpMetricExport{
					{
						Name:        "test-exporter",
						Destination: "https://localhost:9393",
					},
				},
			},
			exporterName: "non-existing",
			wantErr:      true,
			errMsg:       "exporter non-existing not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := GetMetricsExporter(tt.exporterName, tt.exporters)
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, tt.errMsg, err.Error())
				require.Nil(t, client)
			} else {
				require.NoError(t, err)
				require.NotNil(t, client)
			}
		})
	}
}

func TestHasMetricsExporter(t *testing.T) {
	tests := []struct {
		name         string
		exporters    *Exporters
		exporterName string
		want         bool
	}{
		{
			name: "existing exporter",
			exporters: &Exporters{
				OtlpMetricExport: []*OtlpMetricExport{
					{
						Name:        "test-exporter",
						Destination: "https://localhost:9393",
					},
				},
			},
			exporterName: "test-exporter",
			want:         true,
		},
		{
			name:         "nil exporters",
			exporters:    nil,
			exporterName: "test-exporter",
			want:         false,
		},
		{
			name: "non-existing exporter",
			exporters: &Exporters{
				OtlpMetricExport: []*OtlpMetricExport{
					{
						Name:        "test-exporter",
						Destination: "https://localhost:9393",
					},
				},
			},
			exporterName: "non-existing",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasMetricsExporter(tt.exporterName, tt.exporters)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGetLogsExporter(t *testing.T) {
	tests := []struct {
		name         string
		exporters    *Exporters
		exporterName string
		wantErr      bool
		errMsg       string
	}{
		{
			name: "existing exporter",
			exporters: &Exporters{
				FluentForwardLogExport: []*FluentForwardLogExport{
					{
						Name:         "test-exporter",
						Destination:  "tcp://localhost:24224",
						TagAttribute: "log_tag",
					},
				},
			},
			exporterName: "test-exporter",
			wantErr:      false,
		},
		{
			name:         "nil exporters",
			exporters:    nil,
			exporterName: "test-exporter",
			wantErr:      true,
			errMsg:       "exporters config not set",
		},
		{
			name: "non-existing exporter",
			exporters: &Exporters{
				FluentForwardLogExport: []*FluentForwardLogExport{
					{
						Name:         "test-exporter",
						Destination:  "tcp://localhost:24224",
						TagAttribute: "log_tag",
					},
				},
			},
			exporterName: "non-existing",
			wantErr:      true,
			errMsg:       "exporter non-existing not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink, err := GetLogsExporter(tt.exporterName, tt.exporters)
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, tt.errMsg, err.Error())
				require.Nil(t, sink)
			} else {
				require.NoError(t, err)
				require.NotNil(t, sink)
			}
		})
	}
}

func TestHasLogsExporter(t *testing.T) {
	tests := []struct {
		name         string
		exporters    *Exporters
		exporterName string
		want         bool
	}{
		{
			name: "existing exporter",
			exporters: &Exporters{
				FluentForwardLogExport: []*FluentForwardLogExport{
					{
						Name:         "test-exporter",
						Destination:  "tcp://localhost:24224",
						TagAttribute: "log_tag",
					},
				},
			},
			exporterName: "test-exporter",
			want:         true,
		},
		{
			name:         "nil exporters",
			exporters:    nil,
			exporterName: "test-exporter",
			want:         false,
		},
		{
			name: "non-existing exporter",
			exporters: &Exporters{
				FluentForwardLogExport: []*FluentForwardLogExport{
					{
						Name:         "test-exporter",
						Destination:  "tcp://localhost:24224",
						TagAttribute: "log_tag",
					},
				},
			},
			exporterName: "non-existing",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasLogsExporter(tt.exporterName, tt.exporters)
			require.Equal(t, tt.want, got)
		})
	}
}
