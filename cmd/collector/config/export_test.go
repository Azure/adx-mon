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
