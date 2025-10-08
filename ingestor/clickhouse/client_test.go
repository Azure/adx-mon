package clickhouse

import (
	"crypto/tls"
	"net/url"
	"testing"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"
)

func TestBuildOptionsNativeDefaults(t *testing.T) {
	cfg := Config{
		Database: "metrics",
		DSN:      "clickhouse://primary",
	}.withDefaults()

	opts, err := buildOptions(cfg)
	require.NoError(t, err)

	require.Equal(t, clickhouse.Native, opts.Protocol)
	require.Equal(t, []string{"primary:9000"}, opts.Addr)
	require.Equal(t, "metrics", opts.Auth.Database)
	require.Nil(t, opts.TLS)
	require.Equal(t, clickhouse.CompressionLZ4, opts.Compression.Method)
	require.Equal(t, cfg.Client.DialTimeout, opts.DialTimeout)
}

func TestBuildOptionsNativeSecureEnabled(t *testing.T) {
	cfg := Config{
		Database: "metrics",
		DSN:      "clickhouse://primary?secure=1",
	}.withDefaults()

	opts, err := buildOptions(cfg)
	require.NoError(t, err)

	require.NotNil(t, opts.TLS)
	require.Equal(t, uint16(tls.VersionTLS12), opts.TLS.MinVersion)
	require.Equal(t, "primary", opts.TLS.ServerName)
}

func TestBuildOptionsHTTP(t *testing.T) {
	cfg := Config{
		Database: "logs",
		DSN:      "http://endpoint",
	}.withDefaults()

	opts, err := buildOptions(cfg)
	require.NoError(t, err)

	require.Equal(t, clickhouse.HTTP, opts.Protocol)
	require.Equal(t, []string{"endpoint:8123"}, opts.Addr)
	require.Nil(t, opts.TLS)
}

func TestBuildOptionsMultiHost(t *testing.T) {
	cfg := Config{
		Database: "metrics",
		DSN:      "clickhouse://host-a,host-b:9440",
	}.withDefaults()

	opts, err := buildOptions(cfg)
	require.NoError(t, err)

	require.ElementsMatch(t, []string{"host-a:9000", "host-b:9440"}, opts.Addr)
}

func TestBuildOptionsAsyncInsertSettings(t *testing.T) {
	cfg := Config{
		Database: "metrics",
		DSN:      "clickhouse://primary",
		Client: ClientConfig{
			AsyncInsert: true,
		},
	}.withDefaults()

	opts, err := buildOptions(cfg)
	require.NoError(t, err)

	require.Equal(t, 1, opts.Settings["async_insert"])
	require.Equal(t, 1, opts.Settings["wait_for_async_insert"])
}

func TestResolveCompression(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		expected clickhouse.CompressionMethod
		wantErr  bool
	}{
		{name: "default lz4", method: "", expected: clickhouse.CompressionLZ4},
		{name: "explicit lz4", method: "lz4", expected: clickhouse.CompressionLZ4},
		{name: "explicit zstd", method: "zstd", expected: clickhouse.CompressionZSTD},
		{name: "none", method: "none", expected: clickhouse.CompressionNone},
		{name: "invalid", method: "br", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compression, err := resolveCompression(tt.method)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, compression.Method)
		})
	}
}

func TestResolveProtocolErrors(t *testing.T) {
	_, _, err := resolveProtocol("udp")
	require.Error(t, err)
}

func TestExpandAddressesSkipsEmpty(t *testing.T) {
	addrs := expandAddresses("host-a,,host-b", "9000")
	require.Equal(t, []string{"host-a:9000", "host-b:9000"}, addrs)
}

func TestBuildOptionsRequiresDSN(t *testing.T) {
	_, err := buildOptions(Config{Database: "metrics"}.withDefaults())
	require.Error(t, err)
}

func TestTLSConfigWithCustomFiles(t *testing.T) {
	uri, err := url.Parse("https://endpoint")
	require.NoError(t, err)

	tlsCfg, err := buildTLSConfig(TLSConfig{InsecureSkipVerify: true}, uri)
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)
	require.True(t, tlsCfg.InsecureSkipVerify)
}

func TestResolveAuthFallbacks(t *testing.T) {
	t.Setenv("CLICKHOUSE_USER", "env-user")
	t.Setenv("CLICKHOUSE_PASSWORD", "env-pass")

	uri, err := url.Parse("clickhouse://endpoint")
	require.NoError(t, err)

	user, pass := resolveAuth(uri, AuthConfig{})
	require.Equal(t, "env-user", user)
	require.Equal(t, "env-pass", pass)

	uri, err = url.Parse("clickhouse://user:pass@endpoint")
	require.NoError(t, err)

	user, pass = resolveAuth(uri, AuthConfig{Username: "override", Password: "override-pass"})
	require.Equal(t, "user", user)
	require.Equal(t, "pass", pass)
}

func TestBuildOptionsRespectsTimeouts(t *testing.T) {
	cfg := Config{
		Database: "metrics",
		DSN:      "clickhouse://primary",
		Client: ClientConfig{
			DialTimeout: 2 * time.Second,
			ReadTimeout: 45 * time.Second,
		},
	}.withDefaults()

	opts, err := buildOptions(cfg)
	require.NoError(t, err)

	require.Equal(t, 2*time.Second, opts.DialTimeout)
	require.Equal(t, 45*time.Second, opts.ReadTimeout)
}
