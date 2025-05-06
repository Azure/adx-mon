package parser

import (
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/assert"
)

func TestKlogParser_Parse(t *testing.T) {
	parser, err := NewKlogParser(KlogParserConfig{})
	assert.NoError(t, err)

	tests := []struct {
		name     string
		input    string
		expected map[string]any
	}{
		{
			name:  "Valid klog with message and key-value pairs",
			input: "I1025 00:15:15.525108       1 controller_utils.go:116] \"Pod status updated\" pod=\"kube-system/kubedns\" status=\"ready\"",
			expected: map[string]any{
				"timestamp":   "00:15:15.525108",
				"filename":    "controller_utils.go",
				"line_number": "116",
				"message":     "Pod status updated",
				"pod":         "kube-system/kubedns",
				"status":      "ready",
			},
		},
		{
			name:  "Valid klog with message and many key-value pairs",
			input: `I0506 15:38:33.629140       1 proxier.go:1508] "Reloading service iptables data" numServices=0 numEndpoints=0 numFilterChains=5 numFilterRules=3 numNATChains=4 numNATRules=5`,
			expected: map[string]any{
				"timestamp":       "15:38:33.629140",
				"filename":        "proxier.go",
				"line_number":     "1508",
				"message":         "Reloading service iptables data",
				"numServices":     "0",
				"numEndpoints":    "0",
				"numFilterChains": "5",
				"numFilterRules":  "3",
				"numNATChains":    "4",
				"numNATRules":     "5",
			},
		},
		{
			name:  "Valid klog with only message",
			input: "I1025 00:15:15.525108       1 example.go:79] \"This is a message\"",
			expected: map[string]any{
				"timestamp":   "00:15:15.525108",
				"filename":    "example.go",
				"line_number": "79",
				"message":     "This is a message",
			},
		},
		{
			name:  "Unstructured log message",
			input: "I0506 15:40:33.494743       1 bounded_frequency_runner.go:296] sync-runner: ran, next possible in 1s, periodic in 1h0m0s",
			expected: map[string]any{
				"timestamp":   "15:40:33.494743",
				"filename":    "bounded_frequency_runner.go",
				"line_number": "296",
				"message":     "sync-runner: ran, next possible in 1s, periodic in 1h0m0s",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := types.NewLog()
			err := parser.Parse(log, tt.input)
			assert.NoError(t, err)

			for key, expectedValue := range tt.expected {
				actualValue, exists := log.GetBodyValue(key)
				assert.True(t, exists, "Key %s should exist", key)
				assert.Equal(t, expectedValue, actualValue, "Value for key %s should match", key)
			}
		})
	}
}
