package alerter_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
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
	port := freeTCPPort(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	opts := &alerter.AlerterOpts{
		Dev:              true,
		KustoEndpoints:   map[string]string{"fake": fmt.Sprintf("http://127.0.0.1:%d", port)},
		Region:           "test-region",
		AlertAddr:        srv.URL,
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

	ctx := context.Background()
	err = service.Open(ctx)
	require.NoError(t, err)

	err = service.Close()
	require.NoError(t, err)
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

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestNewService_AlertAddrsValidationAndFakeLoopback(t *testing.T) {
	baseOpts := func() *alerter.AlerterOpts {
		return &alerter.AlerterOpts{
			Dev:              true,
			KustoEndpoints:   map[string]string{"fake": "http://fake.endpoint"},
			Region:           "test-region",
			Cloud:            "Azure",
			Port:             freeTCPPort(t),
			Concurrency:      4,
			Tags:             map[string]string{"env": "test"},
			MaxNotifications: 10,
			MSIID:            "test-msi-id",
			MSIResource:      "https://kusto.kusto.windows.net",
			KustoToken:       "test-token",
		}
	}

	t.Run("omitted addresses mounts loopback fakeAlertHandler", func(t *testing.T) {
		opts := baseOpts()
		svc, err := alerter.NewService(opts)
		require.NoError(t, err)
		require.NotNil(t, svc)
		require.Contains(t, opts.AlertAddr, "http://localhost:")
	})

	t.Run("explicit empty AlertAddrs entry fails startup without mounting fake handler", func(t *testing.T) {
		opts := baseOpts()
		opts.AlertAddrs = []string{""}
		svc, err := alerter.NewService(opts)
		require.Error(t, err)
		require.Nil(t, svc)
	})

	t.Run("whitespace-only AlertAddr fails startup", func(t *testing.T) {
		opts := baseOpts()
		opts.AlertAddr = "   "
		svc, err := alerter.NewService(opts)
		require.Error(t, err)
		require.Nil(t, svc)
	})

	t.Run("invalid scheme or missing host fails without leaking raw URL", func(t *testing.T) {
		for _, bad := range []string{
			"ftp://user:SECRET_PASS@hooks.example.com/secret-path?tok=SECRET_TOK",
			"http:///missing-host/secret-path?tok=SECRET_TOK",
			"hooks.example.com/no-scheme/SECRET_TOK",
		} {
			opts := baseOpts()
			opts.AlertAddrs = []string{bad}
			svc, err := alerter.NewService(opts)
			require.Error(t, err)
			require.Nil(t, svc)
			require.NotContains(t, err.Error(), bad)
			require.NotContains(t, err.Error(), "SECRET_PASS")
			require.NotContains(t, err.Error(), "SECRET_TOK")
		}
	})

	t.Run("multiple endpoints preserve commas and queries and redact /metrics", func(t *testing.T) {
		var hitsA, hitsB atomic.Int32
		var uriA, uriB atomic.Value
		srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uriA.Store(r.URL.RequestURI())
			hitsA.Add(1)
			w.WriteHeader(http.StatusCreated)
		}))
		defer srvA.Close()
		srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uriB.Store(r.URL.RequestURI())
			hitsB.Add(1)
			w.WriteHeader(http.StatusCreated)
		}))
		defer srvB.Close()

		opts := baseOpts()
		rawA := srvA.URL + "/team,prod?sig=SECRET_SIG_A"
		rawADup := srvA.URL + "/team,prod/?sig=SECRET_SIG_A"
		rawB := srvB.URL + "/v1/alerts?tok=SECRET_TOK_B"
		opts.AlertAddrs = []string{rawA, rawB, rawADup}

		svc, err := alerter.NewService(opts)
		require.NoError(t, err)
		require.NotNil(t, svc)
		require.NoError(t, svc.Open(context.Background()))

		require.Eventually(t, func() bool {
			return hitsA.Load() >= 1 && hitsB.Load() >= 1
		}, 5*time.Second, 10*time.Millisecond)

		require.NoError(t, svc.Close())

		// Duplicate trailing-slash base URL was deduplicated in order: srvA received 1 POST, not 2.
		require.Equal(t, int32(1), hitsA.Load())
		require.Equal(t, int32(1), hitsB.Load())
		require.Equal(t, "/team,prod/alerts?sig=SECRET_SIG_A", uriA.Load())
		require.Equal(t, "/v1/alerts/alerts?tok=SECRET_TOK_B", uriB.Load())

		mfs, err := prometheus.DefaultGatherer.Gather()
		require.NoError(t, err)
		var buf bytes.Buffer
		for _, mf := range mfs {
			_, err := expfmt.MetricFamilyToText(&buf, mf)
			require.NoError(t, err)
		}
		metricsText := buf.String()
		require.Contains(t, metricsText, `adxmon_alerter_endpoint_queue_depth{endpoint="`+srvA.URL+`/#`)
		require.Contains(t, metricsText, `adxmon_alerter_endpoint_queue_depth{endpoint="`+srvB.URL+`/#`)
		for _, secret := range []string{rawA, rawB, "team,prod", "SECRET_SIG_A", "SECRET_TOK_B", "/v1/alerts"} {
			require.NotContains(t, metricsText, secret)
		}
	})
}
