package testutils

import (
	"log"
	"os"
	"testing"

	"github.com/Azure/adx-mon/pkg/logger"
)

const (
	KustoIntegrationTest             = "KUSTO_INTEGRATION_TEST"
	KustoIntegrationTestSkipTearDown = "KUSTO_INTEGRATION_TEST_SKIP_TEARDOWN"
)

func MainKustoIntegrationTest(m *testing.M) {
	if !truthy(KustoIntegrationTest) {
		log.Println("Kusto integration tests are not enabled, skipping...")
		os.Exit(0)
	}

	if err := StartKusto(); err != nil {
		logger.Fatalf("Failed to start kustainer: %s\n", err)
	}

	code := m.Run()

	if !truthy(KustoIntegrationTestSkipTearDown) {
		if err := StopKusto(); err != nil {
			logger.Errorf("Failed to stop kustainer: %s\n", err)
		}
	}

	os.Exit(code)
}

func KustoIntegrationTestEnabled(t *testing.T) {
	t.Helper()

	if !truthy(KustoIntegrationTest) {
		t.Skip("Kusto integration tests are not enabled, skipping...")
	}
}

func truthy(envVar string) bool {
	v := os.Getenv(envVar)
	switch v {
	case "true", "True", "1", "TRUE":
		return true
	default:
		return false
	}
}
