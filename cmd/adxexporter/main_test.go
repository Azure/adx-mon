package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
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
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
