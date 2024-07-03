package testutils

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/stretchr/testify/require"
)

const (
	KustoLocalEndpoint = "http://localhost:8081"
	DatabaseName       = "OTLPLogs"
)

func StartKusto() error {
	if err := BuildEnv(); err != nil {
		return err
	}

	cmd := exec.Command("docker", "compose", "-f", "../../otlp/logs/compose.yaml", "up", "--detach")
	_, err := Run(cmd)
	if err != nil {
		return fmt.Errorf("failed to start docker compose: %s", err)
	}

	return nil
}

func BuildEnv() error {
	cmd := exec.Command("docker", "compose", "-f", "../../otlp/logs/compose.yaml", "build")
	_, err := Run(cmd)
	if err != nil {
		return fmt.Errorf("failed to build docker compose: %s", err)
	}

	return nil
}

func StopKusto() error {
	cmd := exec.Command("docker", "compose", "-f", "../../otlp/logs/compose.yaml", "down", "--remove-orphans", "--volumes")
	_, err := Run(cmd)
	if err != nil {
		return fmt.Errorf("failed to stop docker compose: %s", err)
	}

	return nil
}

func QueryKusto(t *testing.T, q *kql.Builder, iter func(row *table.Row) error) {
	t.Helper()

	var (
		rows *kusto.RowIterator
		err  error
	)
	sb := kusto.NewConnectionStringBuilder(KustoLocalEndpoint)
	client, err := kusto.New(sb)
	require.NoError(t, err)
	defer client.Close()

	require.Eventually(t, func() bool {
		rows, err = client.Query(context.Background(), DatabaseName, q)
		return err == nil
	}, time.Minute, 5*time.Second)
	defer rows.Stop()

	for {
		row, errInline, errFinal := rows.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			t.Fatalf("failed to retrieve row: %v", errFinal)
		}
		if err := iter(row); err != nil {
			t.Fatalf("failed to parse row: %v", err)
		}
	}
}
