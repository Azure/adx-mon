package testutils

import (
	"os"
	"testing"
)

func IntegrationTest(t *testing.T) {
	t.Helper()
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("skipping integration tests, set environment variable INTEGRATION")
	}
}

func RollbackTest(t *testing.T) {
	t.Helper()
	if os.Getenv("ROLLBACK") == "" {
		t.Skip("skipping rollback tests, set environment variable ROLLBACK")
	}
}
