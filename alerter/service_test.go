package alerter_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
