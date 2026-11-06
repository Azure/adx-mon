package parser

import (
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

func TestParseToJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]any
	}{
		{
			name:     "simple key-value pairs",
			input:    "foo=bar baz=boop",
			expected: map[string]any{"foo": "bar", "baz": "boop"},
		},
		{
			name:     "keys without values",
			input:    "standalone flag another",
			expected: map[string]any{"standalone": "", "flag": "", "another": ""},
		},
		{
			name:     "mixed keys with and without values",
			input:    "key=value standalone another=thing",
			expected: map[string]any{"key": "value", "standalone": "", "another": "thing"},
		},
		{
			name:     "values with spaces",
			input:    "something=something else even=more",
			expected: map[string]any{"something": "something", "else": "", "even": "more"},
		},
		{
			name:     "spaces in input",
			input:    "   spaced   out   key=value   ",
			expected: map[string]any{"spaced": "", "out": "", "key": "value"},
		},
		{
			name:     "empty input",
			input:    "",
			expected: map[string]any{},
		},
		{
			name:     "special characters in values",
			input:    "url=http://example.com path=/some/path",
			expected: map[string]any{"url": "http://example.com", "path": "/some/path"},
		},
		{
			name:     "equals sign in value",
			input:    "equation=1+1=2 key=value",
			expected: map[string]any{"equation": "1+1=2", "key": "value"},
		},
		{
			name:     "multiple equals signs",
			input:    "complex=key=with=equals",
			expected: map[string]any{"complex": "key=with=equals"},
		},
		{
			name:     "duplicate keys",
			input:    "dup=first dup=second other=value",
			expected: map[string]any{"dup": "second", "other": "value"},
		},
		{
			name:     "quoted values are preserved",
			input:    `key="quoted value" other='single quotes'`,
			expected: map[string]any{"key": "quoted value", "other": "single quotes"},
		},
		{
			name:     "numeric values",
			input:    "count=123 float=45.67",
			expected: map[string]any{"count": 123, "float": 45.67},
		},
		{
			name:     "bools values",
			input:    "t=True f=False",
			expected: map[string]any{"t": true, "f": false},
		},
		{
			name:     "invalid number",
			input:    "n=3.2.4.8",
			expected: map[string]any{"n": "3.2.4.8"},
		},
		{
			name:     "preserve empty values",
			input:    "foo= baz=boop",
			expected: map[string]any{"foo": "", "baz": "boop"},
		},
	}

	parser, err := NewKeyValueParser(KeyValueParserConfig{})
	require.NoError(t, err)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := types.NewLog()
			require.NoError(t, parser.Parse(log, tt.input))
			require.Equal(t, tt.expected, log.GetBody())
		})
	}
}

func TestParseToJSON_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]any
	}{
		{
			name:     "only spaces",
			input:    "     ",
			expected: map[string]any{},
		},
		{
			name:     "key with empty value",
			input:    "key=",
			expected: map[string]any{"key": ""},
		},
		{
			name:     "equals sign only",
			input:    "=",
			expected: map[string]any{"": ""},
		},
		{
			name:     "starts with equals sign",
			input:    "=value key2=value2",
			expected: map[string]any{"": "value", "key2": "value2"},
		},
		{
			name:     "special characters in keys",
			input:    "key-with-hyphens=value key_with_underscores=value2",
			expected: map[string]any{"key-with-hyphens": "value", "key_with_underscores": "value2"},
		},
	}

	parser, err := NewKeyValueParser(KeyValueParserConfig{})
	require.NoError(t, err)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := types.NewLog()
			require.NoError(t, parser.Parse(log, tt.input))
			require.Equal(t, tt.expected, log.GetBody())
		})
	}
}
