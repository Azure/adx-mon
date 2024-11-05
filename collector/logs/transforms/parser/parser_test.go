package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewParsers(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []Parser
	}{
		{
			name:     "valid parser",
			input:    []string{"json"},
			expected: []Parser{&JsonParser{}},
		},
		{
			name:     "empty",
			input:    []string{},
			expected: []Parser{},
		},
		{
			name:     "skips invalid",
			input:    []string{"invalid", "json", "invalid2"},
			expected: []Parser{&JsonParser{}},
		},
		{
			name:     "all invalid",
			input:    []string{"invalid", "invalid2"},
			expected: []Parser{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsers := NewParsers(tt.input, "test")
			require.Len(t, parsers, len(tt.expected))
			for i, parser := range parsers {
				require.IsType(t, tt.expected[i], parser)
			}
		})
	}
}

func TestIsValidParser(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid",
			input:    "json",
			expected: true,
		},
		{
			name:     "invalid",
			input:    "invalid",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, IsValidParser(tt.input))
		})
	}
}
