package main

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Azure/adx-mon/cmd/collector/config"
	"github.com/pelletier/go-toml/v2"
)

func TestParseTarget(t *testing.T) {
	for _, tc := range []struct {
		name, target, endpoint, directory string
		invalid                           bool
	}{
		{name: "http", target: "http://localhost:4318", endpoint: "http://localhost:4318/v1/logs"},
		{name: "https", target: "https://example.com/", endpoint: "https://example.com/v1/logs"},
		{name: "base path and query", target: "https://example.com/base/?key=value", endpoint: "https://example.com/base/v1/logs?key=value"},
		{name: "escaped HTTP path", target: "https://example.com/a%2Fb", endpoint: "https://example.com/a%2Fb/v1/logs"},
		{name: "trailing escaped slash", target: "https://example.com/base%2F", endpoint: "https://example.com/base%2F/v1/logs"},
		{name: "trailing lowercase escaped slash", target: "https://example.com/base%2f", endpoint: "https://example.com/base%2f/v1/logs"},
		{name: "escaped and literal trailing slashes", target: "https://example.com/base%2F//?key=value", endpoint: "https://example.com/base%2F/v1/logs?key=value"},
		{name: "multiple trailing escaped slashes", target: "https://example.com/base%2F%2F", endpoint: "https://example.com/base%2F%2F/v1/logs"},
		{name: "IPv6", target: "http://[::1]:4318", endpoint: "http://[::1]:4318/v1/logs"},
		{name: "absolute file", target: "file:///tmp/load", directory: "/tmp/load"},
		{name: "escaped file", target: "file:///tmp/load%20%23%3F%25logs", directory: "/tmp/load #?%logs"},
		{name: "cleaned directory", target: "file:///tmp/load/../logs/", directory: "/tmp/logs"},
		{name: "root directory", target: "file:///", directory: "/"},
		{name: "empty", target: "", invalid: true},
		{name: "missing scheme", target: "localhost:4318", invalid: true},
		{name: "relative", target: "logs", invalid: true},
		{name: "unsupported", target: "ftp://localhost/logs", invalid: true},
		{name: "HTTP no host", target: "http:///path", invalid: true},
		{name: "HTTP opaque", target: "http:localhost", invalid: true},
		{name: "invalid port", target: "https://localhost:bogus", invalid: true},
		{name: "out of range port", target: "http://localhost:65536", invalid: true},
		{name: "zero port", target: "http://localhost:0", invalid: true},
		{name: "empty port", target: "http://localhost:", invalid: true},
		{name: "HTTP fragment", target: "https://localhost/#fragment", invalid: true},
		{name: "HTTP empty fragment", target: "https://localhost/#", invalid: true},
		{name: "ambiguous file host", target: "file://path/basedir", invalid: true},
		{name: "nonlocal file host", target: "file://server/tmp/logs", invalid: true},
		{name: "localhost file host", target: "file://localhost/tmp/logs", invalid: true},
		{name: "file user", target: "file://user@/tmp/logs", invalid: true},
		{name: "file no path", target: "file://", invalid: true},
		{name: "file single slash", target: "file:/tmp/logs", invalid: true},
		{name: "file opaque", target: "file:logs", invalid: true},
		{name: "file network path", target: "file:////server/logs", invalid: true},
		{name: "file query", target: "file:///tmp/logs?x=1", invalid: true},
		{name: "file empty query", target: "file:///tmp/logs?", invalid: true},
		{name: "file fragment", target: "file:///tmp/logs#fragment", invalid: true},
		{name: "file NUL", target: "file:///tmp/logs%00", invalid: true},
		{name: "bad escape", target: "file:///tmp/%zz", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTarget(tc.target)
			if tc.invalid {
				if err == nil {
					t.Fatalf("parseTarget(%q) succeeded: %+v", tc.target, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.endpoint != tc.endpoint || got.directory != tc.directory {
				t.Fatalf("got %+v, want endpoint=%q directory=%q", got, tc.endpoint, tc.directory)
			}
		})
	}
}

func validOptions(directory string) options {
	return options{
		target: fileURL(directory), concurrency: 3, batchSize: 4, totalSamples: 11,
		database: "TestDatabase", table: "TestTable",
	}
}

func fileURL(directory string) string {
	return (&url.URL{Scheme: "file", Path: directory}).String()
}

func TestOptionsValidationHasNoSideEffects(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*options)
	}{
		{"zero concurrency", func(o *options) { o.concurrency = 0 }},
		{"negative concurrency", func(o *options) { o.concurrency = -1 }},
		{"zero batch", func(o *options) { o.batchSize = 0 }},
		{"negative batch", func(o *options) { o.batchSize = -1 }},
		{"negative total", func(o *options) { o.totalSamples = -1 }},
		{"empty database", func(o *options) { o.database = "" }},
		{"blank table", func(o *options) { o.table = "  " }},
		{"relative collector directory", func(o *options) { o.collectorLogDir = "logs" }},
		{"NUL collector directory", func(o *options) { o.collectorLogDir = "/logs\x00" }},
		{"HTTP config", func(o *options) { o.target = "http://localhost:4318"; o.printCollectorConfig = true }},
		{"HTTP collector directory", func(o *options) { o.target = "http://localhost:4318"; o.collectorLogDir = "/logs" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "not-created")
			opts := validOptions(directory)
			tc.modify(&opts)
			if err := run(context.Background(), opts, io.Discard, io.Discard); err == nil {
				t.Fatal("expected validation error")
			}
			if _, err := os.Stat(directory); !os.IsNotExist(err) {
				t.Fatalf("validation created directory: %v", err)
			}
		})
	}
}

func TestCollectorConfig(t *testing.T) {
	for _, override := range []string{"", "/container/load 'logs\"\\\n雪"} {
		t.Run(override, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "load 'logs\"\\\n雪")
			opts := validOptions(directory)
			opts.printCollectorConfig = true
			opts.verbose = true
			opts.database = "DB'\"\\\n雪"
			opts.table = "Table'\"\\\t雪"
			opts.collectorLogDir = override
			var output, diagnostics bytes.Buffer
			if err := run(context.Background(), opts, &output, &diagnostics); err != nil {
				t.Fatal(err)
			}
			if diagnostics.Len() != 0 {
				t.Fatalf("unexpected diagnostics: %s", &diagnostics)
			}
			var cfg config.Config
			if err := toml.Unmarshal(output.Bytes(), &cfg); err != nil {
				t.Fatalf("invalid TOML: %v\n%s", err, &output)
			}
			if len(cfg.HostLog) != 1 || !cfg.HostLog[0].DisableKubeDiscovery {
				t.Fatalf("wrong host-log configuration: %+v", cfg.HostLog)
			}
			targets := cfg.HostLog[0].StaticFileTargets
			if len(targets) != opts.concurrency {
				t.Fatalf("got %d targets", len(targets))
			}
			base := directory
			if override != "" {
				base = override
			}
			for i, target := range targets {
				if target.FilePath != writerPath(base, i) || string(target.LogType) != "plain" ||
					!reflect.DeepEqual(target.Parsers, []string{"json"}) || target.Database != opts.database || target.Table != opts.table {
					t.Fatalf("wrong target %d: %+v", i, target)
				}
			}
			if _, err := os.Stat(directory); !os.IsNotExist(err) {
				t.Fatalf("config mode created output directory: %v", err)
			}
		})
	}
}
