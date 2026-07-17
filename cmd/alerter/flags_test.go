package main

import (
	"reflect"
	"testing"

	"github.com/urfave/cli/v2"
)

func TestParseKustoEndpoints(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		want    map[string]string
		wantErr string
	}{
		{
			name:   "valid repeats",
			values: []string{"primary=https://primary.kusto.windows.net", "secondary=https://secondary.kusto.windows.net"},
			want: map[string]string{
				"primary":   "https://primary.kusto.windows.net",
				"secondary": "https://secondary.kusto.windows.net",
			},
		},
		{
			name:    "no separator",
			values:  []string{"primary"},
			wantErr: "Invalid kusto-endpoint format, expected <name>=<endpoint>",
		},
		{
			name:    "extra separator",
			values:  []string{"primary=https://example.test?x=y"},
			wantErr: "Invalid kusto-endpoint format, expected <name>=<endpoint>",
		},
		{
			name:   "empty name",
			values: []string{"=endpoint"},
			want:   map[string]string{"": "endpoint"},
		},
		{
			name:   "empty endpoint",
			values: []string{"primary="},
			want:   map[string]string{"primary": ""},
		},
		{
			name:   "duplicate last wins",
			values: []string{"primary=first", "primary=second"},
			want:   map[string]string{"primary": "second"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseKustoEndpoints(tt.values)
			assertParseResult(t, got, err, tt.want, tt.wantErr)
		})
	}
}

func TestParseTags(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		want    map[string]string
		wantErr string
	}{
		{
			name:   "valid repeats preserve whitespace",
			values: []string{"team=platform", " spaced key = spaced value "},
			want: map[string]string{
				"team":         "platform",
				" spaced key ": " spaced value ",
			},
		},
		{
			name:    "no separator",
			values:  []string{"team"},
			wantErr: "Invalid tag format, expected <key>=<value>",
		},
		{
			name:    "extra separator",
			values:  []string{"team=platform=infra"},
			wantErr: "Invalid tag format, expected <key>=<value>",
		},
		{
			name:   "empty key",
			values: []string{"=platform"},
			want:   map[string]string{"": "platform"},
		},
		{
			name:   "empty value",
			values: []string{"team="},
			want:   map[string]string{"team": ""},
		},
		{
			name:   "duplicate last wins",
			values: []string{"team=first", "team=second"},
			want:   map[string]string{"team": "second"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTags(tt.values)
			assertParseResult(t, got, err, tt.want, tt.wantErr)
		})
	}
}

func TestAddExecutionTags(t *testing.T) {
	tests := []struct {
		name   string
		tags   map[string]string
		region string
		cloud  string
		want   map[string]string
	}{
		{
			name:   "adds region and cloud",
			tags:   map[string]string{"team": "platform"},
			region: "eastus",
			cloud:  "public",
			want:   map[string]string{"team": "platform", "region": "eastus", "cloud": "public"},
		},
		{
			name:   "overrides explicit tags with empty values",
			tags:   map[string]string{"region": "westus", "cloud": "public"},
			region: "",
			cloud:  "",
			want:   map[string]string{"region": "", "cloud": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addExecutionTags(tt.tags, tt.region, tt.cloud)
			if !reflect.DeepEqual(tt.tags, tt.want) {
				t.Fatalf("addExecutionTags() = %#v, want %#v", tt.tags, tt.want)
			}
		})
	}
}

func assertParseResult(t *testing.T, got map[string]string, err error, want map[string]string, wantErr string) {
	t.Helper()
	if wantErr != "" {
		if err == nil {
			t.Fatalf("expected error %q", wantErr)
		}
		if err.Error() != wantErr {
			t.Fatalf("error = %q, want %q", err.Error(), wantErr)
		}
		exitErr, ok := err.(cli.ExitCoder)
		if !ok {
			t.Fatalf("error type = %T, want cli.ExitCoder", err)
		}
		if exitErr.ExitCode() != 1 {
			t.Fatalf("exit code = %d, want 1", exitErr.ExitCode())
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("result = %#v, want %#v", got, want)
	}
}
