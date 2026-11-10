package config

import (
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
)

const scrapeTLSConfig = `
[prometheus-scrape]
database = 'Metrics'
scrape-interval = 30

[prometheus-scrape.scrape-tls-profiles.exporter]
ca-file = '/etc/exporter/ca.crt'
server-name = 'exporter.test'
username-file = '/etc/exporter/username'
password-file = '/etc/exporter/password'
allowed-namespaces = ['metrics']

[[prometheus-scrape.static-scrape-target]]
host-regex = '.*'
url = 'https://127.0.0.1:9443/metrics'
namespace = 'metrics'
pod = 'exporter'
container = 'exporter'
tls-profile = 'exporter'
`

func TestScrapeTLSProfiles(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*PrometheusScrape)
		wantErr string
	}{
		{name: "valid"},
		// Files are checked when the collector loads the profile, not during configuration validation.
		{name: "missing-files", mutate: func(s *PrometheusScrape) {
			p := s.ScrapeTLSProfiles["exporter"]
			p.PasswordFile = "/does/not/exist"
			s.ScrapeTLSProfiles["exporter"] = p
		}},
		{name: "optional-server-name", mutate: func(s *PrometheusScrape) {
			p := s.ScrapeTLSProfiles["exporter"]
			p.ServerName = ""
			s.ScrapeTLSProfiles["exporter"] = p
		}},
		// Static targets are collector configuration, so namespace permissions do not apply to them.
		{name: "static-target-ignores-allowed-namespaces", mutate: func(s *PrometheusScrape) {
			p := s.ScrapeTLSProfiles["exporter"]
			p.AllowedNamespaces = nil
			s.ScrapeTLSProfiles["exporter"] = p
		}},
		{name: "unknown-profile", mutate: func(s *PrometheusScrape) {
			s.StaticScrapeTarget[0].TLSProfile = "not-configured"
		}, wantErr: `prom-scrape.static-scrape-target[0].tls-profile "not-configured" is not defined`},
		{name: "http-url", mutate: func(s *PrometheusScrape) {
			s.StaticScrapeTarget[0].URL = "http://127.0.0.1:9443/metrics"
		}, wantErr: "prom-scrape.static-scrape-target[0].url: scrape TLS requires an HTTPS URL"},
		{name: "url-credentials", mutate: func(s *PrometheusScrape) {
			s.StaticScrapeTarget[0].URL = "https://user:pass@127.0.0.1:9443/metrics"
		}, wantErr: "prom-scrape.static-scrape-target[0].url: scrape TLS requires an HTTPS URL"},
		{name: "unset-file", mutate: func(s *PrometheusScrape) {
			p := s.ScrapeTLSProfiles["exporter"]
			p.UsernameFile = ""
			s.ScrapeTLSProfiles["exporter"] = p
		}, wantErr: "prom-scrape.scrape-tls-profiles.exporter: ca-file, username-file and password-file must be set"},
		{name: "empty-name", mutate: func(s *PrometheusScrape) {
			s.ScrapeTLSProfiles[""] = s.ScrapeTLSProfiles["exporter"]
		}, wantErr: "prom-scrape.scrape-tls-profiles name must not be empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			require.NoError(t, toml.Unmarshal([]byte(scrapeTLSConfig), &cfg))
			s := cfg.PrometheusScrape
			require.Equal(t, "exporter", s.StaticScrapeTarget[0].TLSProfile)
			require.Equal(t, []string{"metrics"}, s.ScrapeTLSProfiles["exporter"].AllowedNamespaces)
			if tc.mutate != nil {
				tc.mutate(s)
			}
			err := s.Validate()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestScrapeTLSProfilesOmittedByDefault(t *testing.T) {
	var cfg Config
	require.NoError(t, toml.Unmarshal([]byte(`
[prometheus-scrape]
database = 'Metrics'
scrape-interval = 30
static-scrape-target = [
  { host-regex = '.*', url = 'http://exporter:9000/metrics', namespace = 'metrics', pod = 'exporter', container = 'exporter' }
]`), &cfg))
	require.NoError(t, cfg.PrometheusScrape.Validate())
	require.Empty(t, cfg.PrometheusScrape.ScrapeTLSProfiles)
	encoded, err := toml.Marshal(cfg.PrometheusScrape)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "scrape-tls-profiles")
	require.NotContains(t, string(encoded), "tls-profile")
}

func TestScrapeTLSProfilesReplaceVariables(t *testing.T) {
	var cfg Config
	require.NoError(t, toml.Unmarshal([]byte(scrapeTLSConfig), &cfg))
	p := cfg.PrometheusScrape.ScrapeTLSProfiles["exporter"]
	p.ServerName = "$(HOSTNAME).exporter.test"
	cfg.PrometheusScrape.ScrapeTLSProfiles["exporter"] = p

	cfg.ReplaceVariable("$(HOSTNAME)", "node-1")
	cfg.ReplaceVariable("$(HOSTIP)", "10.0.0.1")

	require.Equal(t, "node-1.exporter.test", cfg.PrometheusScrape.ScrapeTLSProfiles["exporter"].ServerName)
	require.Equal(t, "/etc/exporter/ca.crt", cfg.PrometheusScrape.ScrapeTLSProfiles["exporter"].CAFile)
	require.Equal(t, []string{"metrics"}, cfg.PrometheusScrape.ScrapeTLSProfiles["exporter"].AllowedNamespaces)
}
