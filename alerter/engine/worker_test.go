package engine

import (
	"bytes"
	"context"
	"fmt"
	"github.com/Azure/adx-mon/alert"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/logger"
	kerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestWorker_ServerError(t *testing.T) {
	kcli := &fakeKustoClient{
		log:      logger.Default,
		queryErr: fmt.Errorf("Request aborted due to an internal service error"),
	}

	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			t.Logf("Create alert should not be called")
			t.Fail()
			return nil
		},
	}

	w := &worker{
		rule: &rules.Rule{
			Namespace: "namespace",
			Name:      "name",
		},
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
	}

	w.ExecuteQuery(context.Background())
}

func TestWorker_ConnectionReset(t *testing.T) {
	kcli := &fakeKustoClient{
		log:      logger.Default,
		queryErr: kerrors.ES(kerrors.OpQuery, kerrors.KHTTPError, "Post \"https://kusto.fqdn/v2/rest/query\": read tcp 1.2.3.4:56140->5.6.7.8:443: read: connection reset by peer"),
	}

	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			t.Logf("Create alert should not be called")
			t.Fail()
			return nil
		},
	}

	w := &worker{
		rule: &rules.Rule{
			Namespace: "namespace",
			Name:      "name",
		},
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
	}

	w.ExecuteQuery(context.Background())
}

func TestWorker_ContextTimeout(t *testing.T) {
	kcli := &fakeKustoClient{
		log: logger.Default,
	}

	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			t.Logf("Create alert should not be called")
			t.Fail()
			return nil
		},
	}

	w := &worker{
		rule: &rules.Rule{
			Namespace: "namespace",
			Name:      "name",
		},
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
	}

	ctx, _ := context.WithDeadline(context.Background(), time.Now())

	w.ExecuteQuery(ctx)
}

func TestWorker_RequestInvalid(t *testing.T) {
	kcli := &fakeKustoClient{
		log:      logger.Default,
		queryErr: kerrors.HTTP(kerrors.OpQuery, "Bad Request", http.StatusBadRequest, io.NopCloser(bytes.NewBufferString("query")), "Request is invalid and cannot be processed: Semantic error: SEM0001: Arithmetic expression cannot be carried-out between DateTime and StringBuffer"),
	}

	var createCalled bool
	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			createCalled = true
			return nil
		},
	}

	w := &worker{
		rule: &rules.Rule{
			Namespace: "namespace",
			Name:      "name",
		},
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
	}

	w.ExecuteQuery(context.Background())
	require.True(t, createCalled, "Create alert should be called")
}
