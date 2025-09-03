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

func TestLint_FailedCriteria_BadExpression(t *testing.T) {
	// Rule has criteria that won't match provided tags AND an invalid CEL expression
	filename := filepath.Join(t.TempDir(), "alert_rule_bad.yaml")
	sampleRule := `
---
apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: bad-alert
  namespace: namespace
spec:
  database: DB
  interval: 5m
  query: |
    Heartbeat
  autoMitigateAfter: 1h
  destination: "somewhere"
  criteriaExpression: "region == 'eastus'"
`
	require.NoError(t, os.WriteFile(filename, []byte(sampleRule), 0644))

	opts := &alerter.AlerterOpts{
		Dev:            true,
		KustoEndpoints: map[string]string{"DB": "http://fake.endpoint"},
		Region:         "test-region",
		AlertAddr:      "http://localhost:8080",
		Cloud:          "Azure",
		Port:           8080,
		Concurrency:    5,
		// Provide tags that do NOT satisfy criteria (region missing)
		Tags:             map[string]string{"env": "test"},
		MaxNotifications: 10,
		MSIID:            "test-msi-id",
		MSIResource:      "https://kusto.kusto.windows.net",
		KustoToken:       "test-token",
		CtrlCli:          nil,
	}

	ctx := context.Background()
	err := alerter.Lint(ctx, opts, filename)
	require.Error(t, err, "expected lint to fail due to bad criteria expression")
}
