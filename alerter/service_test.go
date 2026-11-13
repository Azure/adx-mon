package alerter_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter"
	"github.com/stretchr/testify/require"
)

func TestNewService(t *testing.T) {
	opts := &alerter.AlerterOpts{
		Dev:              true,
		KustoEndpoints:   map[string]string{"fake": "http://fake.endpoint"},
		Region:           "test-region",
		AlertAddr:        "http://localhost:8080",
		Cloud:            "Azure",
		Port:             8080,
		Concurrency:      5,
		Tags:             map[string]string{"env": "test"},
		MaxNotifications: 10,
		MSIID:            "test-msi-id",
		MSIResource:      "https://kusto.kusto.windows.net",
		KustoToken:       "test-token",
		CtrlCli:          nil,
	}

	service, err := alerter.NewService(opts)
	require.NoError(t, err)
	require.NotNil(t, service)
}

func TestAlerter_OpenClose(t *testing.T) {
	opts := &alerter.AlerterOpts{
		Dev:              true,
		KustoEndpoints:   map[string]string{"fake": "http://fake.endpoint"},
		Region:           "test-region",
		AlertAddr:        "http://localhost:8080",
		Cloud:            "Azure",
		Port:             8080,
		Concurrency:      5,
		Tags:             map[string]string{"env": "test"},
		MaxNotifications: 10,
		MSIID:            "test-msi-id",
		MSIResource:      "https://kusto.kusto.windows.net",
		KustoToken:       "test-token",
		CtrlCli:          nil,
	}

	service, err := alerter.NewService(opts)
	require.NoError(t, err)
	require.NotNil(t, service)

	ctx := context.Background()
	err = service.Open(ctx)
	require.NoError(t, err)

	err = service.Close()
	require.NoError(t, err)
}

// TestAlerter_CloseStopsHTTPServer asserts that Close() actually stops the HTTP
// server that Open() starts, so the bound port and its listener goroutine do not
// leak past Close().
//
// It demonstrates the leak through observable behavior: after Open the alerter
// is listening on its port, and after Close the port must no longer accept
// connections. Before the fix, Open() started the server via a bare
// http.ListenAndServe in a goroutine with no *http.Server and Close() never
// shut it down, so the port stayed accepting connections after Close() and the
// final assertion failed. After the fix, Close() calls Shutdown on the stored
// *http.Server, the listener closes, and a dial to the port is refused.
func TestAlerter_CloseStopsHTTPServer(t *testing.T) {
	// Pick a free port to avoid collisions with other tests or processes.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())

	opts := &alerter.AlerterOpts{
		Dev:              true,
		KustoEndpoints:   map[string]string{"fake": "http://fake.endpoint"},
		Region:           "test-region",
		AlertAddr:        fmt.Sprintf("http://localhost:%d", port),
		Cloud:            "Azure",
		Port:             port,
		Concurrency:      5,
		Tags:             map[string]string{"env": "test"},
		MaxNotifications: 10,
		MSIID:            "test-msi-id",
		MSIResource:      "https://kusto.kusto.windows.net",
		KustoToken:       "test-token",
		CtrlCli:          nil,
	}

	service, err := alerter.NewService(opts)
	require.NoError(t, err)
	require.NotNil(t, service)

	require.NoError(t, service.Open(context.Background()))

	// The server binds in a goroutine, so poll briefly until it is reachable.
	require.True(t,
		waitForDial(t, "127.0.0.1", port),
		"expected the alerter HTTP server to be listening on port %d after Open", port)

	require.NoError(t, service.Close())

	// After Close the listener must be stopped: dialing the port is refused.
	require.False(t, dialable("127.0.0.1", port), "expected the alerter HTTP server to be shut down after Close, but port %d is still accepting connections", port)
}

func dialable(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(port)), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// waitForDial polls until a dial to the port succeeds or the deadline passes.
func waitForDial(t *testing.T, host string, port int) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if dialable(host, port) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return dialable(host, port)
}

func TestLint(t *testing.T) {

	filename := filepath.Join(t.TempDir(), "alert_rule.yaml")
	sampleRule := `
---
apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: alert-name-all-criteria
  namespace: namespace
spec:
  database: DB
  interval: 5m
  query: |
    Heartbeat
  criteria:
    env:
    - test
  criteriaExpression: "env == 'test'"
  autoMitigateAfter: 1h
  destination: "somewhere"
---
apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: alert-name-only-expression
  namespace: namespace
spec:
  database: DB
  interval: 5m
  query: |
    Heartbeat
  criteriaExpression: "env == 'test'"
  autoMitigateAfter: 1h
  destination: "somewhere"
---
apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: alert-name-only-criteria
  namespace: namespace
spec:
  database: DB
  interval: 5m
  query: |
    Heartbeat
  criteria:
    env:
    - test
  autoMitigateAfter: 1h
  destination: "somewhere"
`
	require.NoError(t, os.WriteFile(filename, []byte(sampleRule), 0644))

	opts := &alerter.AlerterOpts{
		Dev:              true,
		KustoEndpoints:   map[string]string{"DB": "http://fake.endpoint"},
		Region:           "test-region",
		AlertAddr:        "http://localhost:8080",
		Cloud:            "Azure",
		Port:             8080,
		Concurrency:      5,
		Tags:             map[string]string{"env": "test"},
		MaxNotifications: 10,
		MSIID:            "test-msi-id",
		MSIResource:      "https://kusto.kusto.windows.net",
		KustoToken:       "test-token",
		CtrlCli:          nil,
	}

	ctx := context.Background()
	err := alerter.Lint(ctx, opts, filename)
	require.NoError(t, err)
}

func TestLint_criteriaExpression(t *testing.T) {
	// Table-driven tests covering various invalid/valid criteriaExpression scenarios.
	// Only the rule definition varies; the alerter config (tags, region, etc.) stays constant.
	tests := []struct {
		name      string
		ruleYAML  string
		wantError bool
	}{
		{
			name: "undefined variable (foo)",
			ruleYAML: `---
apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: bad-alert-undefined-var
  namespace: namespace
spec:
  database: DB
  interval: 5m
  query: |
    Heartbeat
  autoMitigateAfter: 1h
  destination: "somewhere"
  criteriaExpression: "foo == 'eastus'"`,
			wantError: true,
		},
		{
			name: "syntax error",
			ruleYAML: `---
apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: bad-alert-syntax
  namespace: namespace
spec:
  database: DB
  interval: 5m
  query: |
    Heartbeat
  autoMitigateAfter: 1h
  destination: "somewhere"
  criteriaExpression: "env == 'test' &&"`,
			wantError: true,
		},
		{
			name: "missing region variable",
			ruleYAML: `---
apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: bad-alert-missing-region
  namespace: namespace
spec:
  database: DB
  interval: 5m
  query: |
    Heartbeat
  autoMitigateAfter: 1h
  destination: "somewhere"
  criteriaExpression: "region == 'test-region'"`,
			wantError: true, // region not provided as a tag, so undefined variable
		},
		{
			name: "valid expression",
			ruleYAML: `---
apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: good-alert-valid
  namespace: namespace
spec:
  database: DB
  interval: 5m
  query: |
    Heartbeat
  autoMitigateAfter: 1h
  destination: "somewhere"
  criteriaExpression: "env == 'prod'"`,
			wantError: false, // not a matching expression, but parses and evaluates fine
		},
	}

	opts := &alerter.AlerterOpts{
		Dev:              true,
		KustoEndpoints:   map[string]string{"DB": "http://fake.endpoint"},
		Region:           "test-region",
		AlertAddr:        "http://localhost:8080",
		Cloud:            "Azure",
		Port:             8080,
		Concurrency:      5,
		Tags:             map[string]string{"env": "test"},
		MaxNotifications: 10,
		MSIID:            "test-msi-id",
		MSIResource:      "https://kusto.kusto.windows.net",
		KustoToken:       "test-token",
		CtrlCli:          nil,
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			filename := filepath.Join(dir, "alert_rule.yaml")
			require.NoError(t, os.WriteFile(filename, []byte(tc.ruleYAML), 0o644))

			ctx := context.Background()
			err := alerter.Lint(ctx, opts, filename)
			if tc.wantError {
				require.Error(t, err, "expected an error for %s", tc.name)
			} else {
				require.NoError(t, err, "did not expect an error for %s", tc.name)
			}
		})
	}
}
