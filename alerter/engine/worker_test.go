package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/metrics"
	kerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

const (
	// Query is successful or only has user-caused errors (invalid queries, etc.)
	QueryHealthHealthy = float64(1)
	// Query is failing due to service issues (unable to query due to networking issues, timeouts, etc)
	QueryHealthUnhealthy = float64(0)
)

func TestWorker_TagsMismatch(t *testing.T) {
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, *table.Row) error) (error, int) {
			t.Logf("Query should not be called")
			t.Fail()
			return nil, 0
		},
	}

	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			t.Logf("Create alert should not be called")
			t.Fail()
			return nil
		},
	}

	rule := &rules.Rule{
		Namespace: "namespace",
		Name:      "name",
		Criteria: map[string][]string{
			"region": {"eastus"},
		},
	}
	w := &worker{
		rule:        rule,
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
		tags: map[string]string{
			"region": "westus",
		},
	}

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, QueryHealthHealthy, gaugeValue)
}

func TestWorker_TagsAtLeastOne(t *testing.T) {
	var queryCalled bool
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, *table.Row) error) (error, int) {
			queryCalled = true
			return nil, 0
		},
	}

	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			t.Logf("Create alert should not be called")
			t.Fail()
			return nil
		},
	}

	rule := &rules.Rule{
		Namespace: "namespace",
		Name:      "name",
		Criteria: map[string][]string{
			"region": {"eastus"},
		},
	}
	w := &worker{
		rule:        rule,
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
		tags: map[string]string{
			"region": "eastus",
			"env":    "prod",
		},
	}

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, QueryHealthHealthy, gaugeValue)
	require.Equal(t, true, queryCalled)
}

func TestWorker_TagsNoneMatch(t *testing.T) {
	var queryCalled bool
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, *table.Row) error) (error, int) {
			queryCalled = true
			return nil, 0
		},
	}

	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			t.Logf("Create alert should not be called")
			t.Fail()
			return nil
		},
	}

	rule := &rules.Rule{
		Namespace: "namespace",
		Name:      "name",
		Criteria: map[string][]string{
			"region": {"westus"},
		},
	}
	w := &worker{
		rule:        rule,
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
		tags: map[string]string{
			"region": "eastus",
			"env":    "prod",
		},
	}

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, QueryHealthHealthy, gaugeValue)
	require.Equal(t, false, queryCalled)
}

func TestWorker_TagsMultiple(t *testing.T) {
	var queryCalled bool
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, *table.Row) error) (error, int) {
			queryCalled = true
			return nil, 0
		},
	}

	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			t.Logf("Create alert should not be called")
			t.Fail()
			return nil
		},
	}

	rule := &rules.Rule{
		Namespace: "namespace",
		Name:      "name",
		Criteria: map[string][]string{
			"region": {"eastus", "westus"},
		},
	}
	w := &worker{
		rule:        rule,
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
		tags: map[string]string{
			"region": "eastus",
			"env":    "prod",
		},
	}

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, QueryHealthHealthy, gaugeValue)
	require.Equal(t, true, queryCalled)

	w = &worker{
		rule:        rule,
		Region:      "westus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
		tags: map[string]string{
			"region": "eastus",
			"env":    "prod",
		},
	}

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	gaugeValue = getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, QueryHealthHealthy, gaugeValue)
	require.Equal(t, true, queryCalled)

}

func TestWorker_ServerError(t *testing.T) {

	kcli := &fakeKustoClient{
		queryErr: fmt.Errorf("Request aborted due to an internal service error"),
	}

	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			t.Logf("Create alert should not be called")
			t.Fail()
			return nil
		},
	}

	rule := &rules.Rule{
		Namespace: "namespace",
		Name:      "name",
	}
	w := &worker{
		rule:        rule,
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
	}

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, QueryHealthUnhealthy, gaugeValue)
}

func TestWorker_ConnectionReset(t *testing.T) {
	kcli := &fakeKustoClient{
		queryErr: kerrors.ES(kerrors.OpQuery, kerrors.KHTTPError, "Post \"https://kusto.fqdn/v2/rest/query\": read tcp 1.2.3.4:56140->5.6.7.8:443: read: connection reset by peer"),
	}

	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			t.Logf("Create alert should not be called")
			t.Fail()
			return nil
		},
	}

	rule := &rules.Rule{
		Namespace: "namespace",
		Name:      "name",
	}
	w := &worker{
		rule:        rule,
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
	}

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, QueryHealthUnhealthy, gaugeValue)
}

func TestWorker_ContextTimeout(t *testing.T) {
	kcli := &fakeKustoClient{
		// fakeKustoClient does not evaluate if context is done, so we need to simulate the error
		queryErr: context.DeadlineExceeded,
	}

	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			t.Logf("Create alert should not be called")
			t.Fail()
			return nil
		},
	}

	rule := &rules.Rule{
		Namespace: "namespace",
		Name:      "name",
	}
	w := &worker{
		rule:        rule,
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
	}

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now())
	defer cancel()

	w.ExecuteQuery(ctx)
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, QueryHealthUnhealthy, gaugeValue)
}

func TestWorker_RequestInvalid(t *testing.T) {
	kcli := &fakeKustoClient{
		queryErr: kerrors.HTTP(kerrors.OpQuery, "Bad Request", http.StatusBadRequest, io.NopCloser(bytes.NewBufferString("query")), "Request is invalid and cannot be processed: Semantic error: SEM0001: Arithmetic expression cannot be carried-out between DateTime and StringBuffer"),
	}

	var createCalled bool
	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			createCalled = true
			return nil
		},
	}

	rule := &rules.Rule{
		Namespace: "namespace",
		Name:      "name",
	}
	w := &worker{
		rule:        rule,
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
	}

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	require.True(t, createCalled, "Create alert should be called")
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	// user caused error
	require.Equal(t, QueryHealthHealthy, gaugeValue)
}

func TestWorker_UnknownDB(t *testing.T) {
	kcli := &fakeKustoClient{
		queryErr: &UnknownDBError{"fakedb"},
	}

	var createCalled bool
	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			createCalled = true
			return nil
		},
	}

	rule := &rules.Rule{
		Namespace: "namespace",
		Name:      "name",
	}
	w := &worker{
		rule:        rule,
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
	}

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	require.True(t, createCalled, "Create alert should be called")
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	// user caused error
	require.Equal(t, QueryHealthHealthy, gaugeValue)
}

func TestWorker_MissingColumnsFromResults(t *testing.T) {
	kcli := &fakeKustoClient{
		queryErr: &NotificationValidationError{"invalid result"},
	}

	var createCalled bool
	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			createCalled = true
			return nil
		},
	}

	rule := &rules.Rule{
		Namespace: "namespace",
		Name:      "name",
	}
	w := &worker{
		rule:        rule,
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
	}

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	require.True(t, createCalled, "Create alert should be called")
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	// user caused error
	require.Equal(t, QueryHealthHealthy, gaugeValue)
}

func TestWorker_AlertsThrottled(t *testing.T) {
	kcli := &fakeKustoClient{
		queryErr: alert.ErrTooManyRequests,
	}

	var createdAlert alert.Alert
	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			createdAlert = alert
			return nil
		},
	}

	rule := &rules.Rule{
		Namespace:   "namespace",
		Name:        "name",
		Destination: "destination/queue",
	}
	w := &worker{
		rule:        rule,
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
	}

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	// query should be healthy - notification was throttled.
	require.Equal(t, QueryHealthHealthy, gaugeValue)

	require.Equal(t, createdAlert.Destination, rule.Destination)
	require.Contains(t, createdAlert.Title, "has too many notifications")
}

func getGaugeValue(t *testing.T, metric prometheus.Metric) float64 {
	t.Helper()

	metricDTO := &dto.Metric{}
	err := metric.Write(metricDTO)
	require.NoError(t, err)
	return metricDTO.Gauge.GetValue()
}
