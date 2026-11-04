package main

import (
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type destination struct {
	endpoint  string
	directory string
}

func parseTarget(raw string) (destination, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return destination{}, fmt.Errorf("invalid target: %w", err)
	}
	if u.Opaque != "" || strings.Contains(raw, "#") || strings.ContainsRune(u.Path, 0) {
		return destination{}, fmt.Errorf("target must be a URL without an opaque path, fragment, or NUL")
	}
	switch u.Scheme {
	case "http", "https":
		if u.Hostname() == "" {
			return destination{}, fmt.Errorf("HTTP target requires a host")
		}
		if port := u.Port(); port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n <= 0 || n > 65535 {
				return destination{}, fmt.Errorf("invalid HTTP target port %q", port)
			}
		} else if strings.HasSuffix(u.Host, ":") {
			return destination{}, fmt.Errorf("HTTP target has an empty port")
		}
		// Trim only literal trailing separators, not encoded slashes (%2F).
		// Derive both path representations from the same escaped path so RawPath
		// remains valid and URL.String preserves the original routing semantics.
		u.RawPath = strings.TrimRight(u.EscapedPath(), "/") + "/v1/logs"
		u.Path, err = url.PathUnescape(u.RawPath)
		if err != nil {
			return destination{}, fmt.Errorf("invalid HTTP target path: %w", err)
		}
		return destination{endpoint: u.String()}, nil
	case "file":
		if !strings.HasPrefix(raw, "file:///") || u.Host != "" || u.User != nil || !filepath.IsAbs(u.Path) || strings.HasPrefix(u.Path, "//") {
			return destination{}, fmt.Errorf("file target must use file:///absolute/directory with no host")
		}
		if u.RawQuery != "" || u.ForceQuery {
			return destination{}, fmt.Errorf("file target must not contain a query")
		}
		return destination{directory: filepath.Clean(u.Path)}, nil
	default:
		return destination{}, fmt.Errorf("unsupported target scheme %q; use http, https, or file", u.Scheme)
	}
}

func (o options) validate() (destination, error) {
	if o.concurrency <= 0 {
		return destination{}, fmt.Errorf("concurrency must be positive")
	}
	if o.batchSize <= 0 {
		return destination{}, fmt.Errorf("batch-size must be positive")
	}
	if o.totalSamples < 0 {
		return destination{}, fmt.Errorf("total-samples must be nonnegative")
	}
	if strings.TrimSpace(o.database) == "" || strings.TrimSpace(o.table) == "" {
		return destination{}, fmt.Errorf("database and table must not be empty")
	}
	target, err := parseTarget(o.target)
	if err != nil {
		return destination{}, err
	}
	if target.directory == "" && (o.printCollectorConfig || o.collectorLogDir != "") {
		return destination{}, fmt.Errorf("collector configuration options require a file target")
	}
	if o.collectorLogDir != "" && (!filepath.IsAbs(o.collectorLogDir) || strings.ContainsRune(o.collectorLogDir, 0)) {
		return destination{}, fmt.Errorf("collector-log-dir must be an absolute directory path without NUL")
	}
	return target, nil
}

func writerPath(directory string, index int) string {
	return filepath.Join(directory, fmt.Sprintf("writer-%d.log", index))
}

func printCollectorConfig(output io.Writer, opts options, target destination) error {
	// Keep this fragment small; it is not a standalone collector configuration.
	type fileTarget struct {
		FilePath string   `toml:"file-path"`
		LogType  string   `toml:"log-type"`
		Parsers  []string `toml:"parsers"`
		Database string   `toml:"database"`
		Table    string   `toml:"table"`
	}
	type hostLog struct {
		DisableKubeDiscovery bool         `toml:"disable-kube-discovery"`
		FileTargets          []fileTarget `toml:"file-target"`
	}
	fragment := struct {
		HostLog []hostLog `toml:"host-log"`
	}{HostLog: []hostLog{{DisableKubeDiscovery: true}}}
	directory := target.directory
	if opts.collectorLogDir != "" {
		directory = opts.collectorLogDir
	}
	for i := 0; i < opts.concurrency; i++ {
		fragment.HostLog[0].FileTargets = append(fragment.HostLog[0].FileTargets, fileTarget{
			FilePath: writerPath(directory, i), LogType: "plain", Parsers: []string{"json"},
			Database: opts.database, Table: opts.table,
		})
	}
	return toml.NewEncoder(output).Encode(fragment)
}
