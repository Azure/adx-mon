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
  name: alert-name
  namespace: namespace
spec:
  database: DB
  interval: 5m
  query: |
    Heartbeat
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
