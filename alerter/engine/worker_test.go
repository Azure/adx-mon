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

	// Notifications are healthy
	NotificationHealthHealthy = float64(0)
	// Notifications are failing
	NotificationHealthUnhealthy = float64(1)
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

func TestWorker_CriteriaExpression_ExecutesOnMatch(t *testing.T) {
	var queryCalled bool
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, *table.Row) error) (error, int) {
			queryCalled = true
			return nil, 0
		},
	}

	alertCli := &fakeAlerter{createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error { return nil }}

	rule := &rules.Rule{
		Namespace:          "namespace",
		Name:               "expr",
		Criteria:           map[string][]string{"region": []string{"nomatch"}}, // won't match
		CriteriaExpression: "cloud == 'public' && region == 'eastus'",
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
			"cloud":  "public",
		},
	}
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)
	w.ExecuteQuery(context.Background())
	require.True(t, queryCalled, "expected query to execute due to CEL expression match")
}
func TestWorker_CriteriaExpression_ExecutesOnMatchTwo(t *testing.T) {
	var queryCalled bool
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, *table.Row) error) (error, int) {
			queryCalled = true
			return nil, 0
		},
	}

	alertCli := &fakeAlerter{createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error { return nil }}

	rule := &rules.Rule{
		Namespace:          "namespace",
		Name:               "expr",
		Criteria:           map[string][]string{"region": []string{"nomatch"}}, // won't match
		CriteriaExpression: "cloud in ['other', 'public'] && region == 'eastus' && environment != 'integration'",
	}
	w := &worker{
		rule:        rule,
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "",
		AlertCli:    alertCli,
		HandlerFn:   nil,
		tags: map[string]string{
			"region":      "eastus",
			"cloud":       "public",
			"environment": "production",
		},
	}
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)
	w.ExecuteQuery(context.Background())
	require.True(t, queryCalled, "expected query to execute due to CEL expression match")
}

func TestWorker_CriteriaExpression_SkipsOnNoMatch(t *testing.T) {
	var queryCalled bool
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, *table.Row) error) (error, int) {
			queryCalled = true
			return nil, 0
		},
	}

	alertCli := &fakeAlerter{createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error { return nil }}

	rule := &rules.Rule{
		Namespace:          "namespace",
		Name:               "expr2",
		Criteria:           map[string][]string{"region": []string{"nomatch"}}, // won't match
		CriteriaExpression: "cloud == 'public' && region == 'westus'",          // region mismatch
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
			"cloud":  "public",
		},
	}
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)
	w.ExecuteQuery(context.Background())
	require.False(t, queryCalled, "expected query NOT to execute due to CEL expression evaluating false and criteria mismatch")
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
	require.Contains(t, createdAlert.Title, "has too many notifications in eastus")
}

func TestWorker_NotificationHealth(t *testing.T) {
	kcli := &fakeKustoClient{
		queryErr: &NotificationValidationError{"invalid result"},
	}

	// First test: Alert creation fails
	var createCalled bool
	alertCli := &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			createCalled = true
			return fmt.Errorf("failed to create alert")
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

	// Initialize metrics to healthy state
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)
	metrics.NotificationUnhealthy.WithLabelValues(rule.Namespace, rule.Name).Set(NotificationHealthHealthy)

	w.ExecuteQuery(context.Background())

	// Verify alert creation was attempted
	require.True(t, createCalled, "Create alert should be called")

	// Verify notification health is now unhealthy (create failed)
	notificationHealthValue := getGaugeValue(t, metrics.NotificationUnhealthy.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, NotificationHealthUnhealthy, notificationHealthValue)

	// Second test: Alert creation succeeds
	createCalled = false
	alertCli = &fakeAlerter{
		createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error {
			createCalled = true
			return nil
		},
	}

	w.AlertCli = alertCli

	// Keep metrics as they were after the first test
	w.ExecuteQuery(context.Background())

	// Verify alert creation was attempted
	require.True(t, createCalled, "Create alert should be called")

	// Verify notification health is now healthy (create succeeded)
	notificationHealthValue = getGaugeValue(t, metrics.NotificationUnhealthy.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, NotificationHealthHealthy, notificationHealthValue)
}

func TestCalculateNextQueryTime(t *testing.T) {
	now := time.Now()
	interval := 5 * time.Minute

	t.Run("first execution returns immediate", func(t *testing.T) {
		w := &worker{
			rule: &rules.Rule{
				Namespace:     "ns",
				Name:          "rule",
				Interval:      interval,
				LastQueryTime: time.Time{}, // zero value
			},
		}
		result := w.calculateNextQueryTime()
		// Should be in the past (immediate execution)
		require.True(t, result.Before(time.Now().Add(1*time.Second)), "expected immediate execution")
	})

	t.Run("scheduled execution returns lastQueryTime+interval", func(t *testing.T) {
		last := now.Add(-10 * time.Minute)
		w := &worker{
			rule: &rules.Rule{
				Namespace:     "ns",
				Name:          "rule",
				Interval:      interval,
				LastQueryTime: last,
			},
		}
		result := w.calculateNextQueryTime()
		expected := last.Add(interval)
		require.Equal(t, expected, result)
	})
}

func getGaugeValue(t *testing.T, metric prometheus.Metric) float64 {
	t.Helper()

	metricDTO := &dto.Metric{}
	err := metric.Write(metricDTO)
	require.NoError(t, err)
	return metricDTO.Gauge.GetValue()
}
