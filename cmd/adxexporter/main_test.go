package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseKustoEndpoints(t *testing.T) {
	tests := []struct {
		name          string
		endpoints     []string
		expected      map[string]string
		expectedError bool
	}{
		{
			name:      "empty endpoints",
			endpoints: []string{},
			expected:  map[string]string{},
		},
		{
			name:      "single valid endpoint",
			endpoints: []string{"metrics=https://cluster1.kusto.windows.net"},
			expected: map[string]string{
				"metrics": "https://cluster1.kusto.windows.net",
			},
		},
		{
			name: "multiple valid endpoints",
			endpoints: []string{
				"metrics=https://cluster1.kusto.windows.net",
				"logs=https://cluster2.kusto.windows.net",
			},
			expected: map[string]string{
				"metrics": "https://cluster1.kusto.windows.net",
				"logs":    "https://cluster2.kusto.windows.net",
			},
		},
		{
			name:          "invalid format - missing equals",
			endpoints:     []string{"metrics-https://cluster.kusto.windows.net"},
			expectedError: true,
		},
		{
			name:          "invalid format - empty database",
			endpoints:     []string{"=https://cluster.kusto.windows.net"},
			expectedError: true,
		},
		{
			name:          "invalid format - empty endpoint",
			endpoints:     []string{"metrics="},
			expectedError: true,
		},
		{
			name: "mixed valid and invalid - should fail",
			endpoints: []string{
				"metrics=https://cluster1.kusto.windows.net",
				"invalid-endpoint",
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseKustoEndpoints(tt.endpoints)

			if tt.expectedError {
				require.Error(t, err)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestParseClusterLabels(t *testing.T) {
	tests := []struct {
		name          string
		labels        []string
		expected      map[string]string
		expectedError bool
	}{
		{
			name:     "empty labels",
			labels:   []string{},
			expected: map[string]string{},
		},
		{
			name:   "single valid label",
			labels: []string{"env=prod"},
			expected: map[string]string{
				"env": "prod",
			},
		},
		{
			name: "multiple valid labels",
			labels: []string{
				"env=prod",
				"region=us-east",
			},
			expected: map[string]string{
				"env":    "prod",
				"region": "us-east",
			},
		},
		{
			name:          "invalid format - missing equals",
			labels:        []string{"env-prod"},
			expectedError: true,
		},
		{
			name: "mixed valid and invalid - should fail",
			labels: []string{
				"env=prod",
				"invalid-label",
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseClusterLabels(tt.labels)

			if tt.expectedError {
				require.Error(t, err)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestParseKeyValuePairs(t *testing.T) {
	tests := []struct {
		name          string
		pairs         []string
		expected      map[string]string
		expectedError bool
	}{
		{
			name:     "empty pairs",
			pairs:    []string{},
			expected: map[string]string{},
		},
		{
			name:  "single valid pair",
			pairs: []string{"service.name=adxexporter"},
			expected: map[string]string{
				"service.name": "adxexporter",
			},
		},
		{
			name: "multiple valid pairs with dots in keys",
			pairs: []string{
				"service.name=adxexporter",
				"service.version=1.0.0",
				"deployment.environment=production",
			},
			expected: map[string]string{
				"service.name":           "adxexporter",
				"service.version":        "1.0.0",
				"deployment.environment": "production",
			},
		},
		{
			name:  "value with equals sign",
			pairs: []string{"url=http://localhost:8080?param=value"},
			expected: map[string]string{
				"url": "http://localhost:8080?param=value",
			},
		},
		{
			name:          "invalid format - missing equals",
			pairs:         []string{"invalid-pair"},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseKeyValuePairs(tt.pairs)

			if tt.expectedError {
				require.Error(t, err)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}
