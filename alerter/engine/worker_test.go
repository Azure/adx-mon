package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/metrics"
	kerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	aztypes "github.com/Azure/azure-kusto-go/azkustodata/types"
	azvalue "github.com/Azure/azure-kusto-go/azkustodata/value"
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
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
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
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", Tags: map[string]string{"region": "westus"}, KustoClient: kcli, AlertClient: alertCli})

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, QueryHealthHealthy, gaugeValue)
}

func TestWorker_TagsAtLeastOne(t *testing.T) {
	var queryCalled bool
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
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
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", Tags: map[string]string{"region": "eastus", "env": "prod"}, KustoClient: kcli, AlertClient: alertCli})

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, QueryHealthHealthy, gaugeValue)
	require.Equal(t, true, queryCalled)
}

func TestWorker_ExecuteQuery_StopsWaitingForSlotOnCancel(t *testing.T) {
	queryCalled := false
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			queryCalled = true
			return nil, 0
		},
	}

	rule := &rules.Rule{
		Namespace: "namespace",
		Name:      "name",
	}
	w := NewWorker(&WorkerConfig{
		Rule:                 rule,
		Region:               "eastus",
		KustoClient:          kcli,
		MaxConcurrentQueries: 1,
		AlertClient:          &fakeAlerter{},
	})
	w.querySlots <- struct{}{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.ExecuteQuery(ctx)
	}()

	select {
	case <-done:
		t.Fatal("ExecuteQuery returned before cancellation while waiting for a query slot")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ExecuteQuery blocked while waiting for a query slot after cancellation")
	}

	require.False(t, queryCalled)
	require.Equal(t, 1, len(w.querySlots))
}

func TestWorker_ExecuteQuery_DoesNotAcquireSlotWhenAlreadyCanceled(t *testing.T) {
	queryCalled := false
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			queryCalled = true
			return nil, 0
		},
	}

	rule := &rules.Rule{
		Namespace: "namespace",
		Name:      "name",
	}
	w := NewWorker(&WorkerConfig{
		Rule:                 rule,
		Region:               "eastus",
		KustoClient:          kcli,
		MaxConcurrentQueries: 1,
		AlertClient:          &fakeAlerter{},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w.ExecuteQuery(ctx)

	require.False(t, queryCalled)
	require.Empty(t, w.querySlots)
}

func TestWorker_TagsNoneMatch(t *testing.T) {
	var queryCalled bool
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
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
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", Tags: map[string]string{"region": "eastus", "env": "prod"}, KustoClient: kcli, AlertClient: alertCli})

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
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
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
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", Tags: map[string]string{"region": "eastus", "env": "prod"}, KustoClient: kcli, AlertClient: alertCli})

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, QueryHealthHealthy, gaugeValue)
	require.Equal(t, true, queryCalled)

	w = NewWorker(&WorkerConfig{Rule: rule, Region: "westus", Tags: map[string]string{"region": "eastus", "env": "prod"}, KustoClient: kcli, AlertClient: alertCli})

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
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			queryCalled = true
			return nil, 0
		},
	}

	alertCli := &fakeAlerter{createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error { return nil }}

	rule := &rules.Rule{
		Namespace:          "namespace",
		Name:               "expr",
		CriteriaExpression: "cloud == 'public' && region == 'eastus'",
	}
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", Tags: map[string]string{"region": "eastus", "cloud": "public"}, KustoClient: kcli, AlertClient: alertCli})
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)
	w.ExecuteQuery(context.Background())
	require.True(t, queryCalled, "expected query to execute due to CEL expression match")
}
func TestWorker_CriteriaExpression_ExecutesOnMatchTwo(t *testing.T) {
	var queryCalled bool
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			queryCalled = true
			return nil, 0
		},
	}

	alertCli := &fakeAlerter{createFn: func(ctx context.Context, endpoint string, alert alert.Alert) error { return nil }}

	rule := &rules.Rule{
		Namespace:          "namespace",
		Name:               "expr",
		CriteriaExpression: "cloud in ['other', 'public'] && region == 'eastus' && environment != 'integration'",
	}
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", Tags: map[string]string{"region": "eastus", "cloud": "public", "environment": "production"}, KustoClient: kcli, AlertClient: alertCli})
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)
	w.ExecuteQuery(context.Background())
	require.True(t, queryCalled, "expected query to execute due to CEL expression match")
}

func TestWorker_CriteriaExpression_SkipsOnNoMatch(t *testing.T) {
	var queryCalled bool
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
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
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", Tags: map[string]string{"region": "eastus", "cloud": "public"}, KustoClient: kcli, AlertClient: alertCli})
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
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", KustoClient: kcli, AlertClient: alertCli})

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
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", KustoClient: kcli, AlertClient: alertCli})

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, QueryHealthUnhealthy, gaugeValue)
}

func TestWorker_ContextTimeout(t *testing.T) {
	kcli := &fakeKustoClient{
		// fakeKustoClient does not evaluate context deadlines, so we simulate the timeout error directly.
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
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", KustoClient: kcli, AlertClient: alertCli})

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
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
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", KustoClient: kcli, AlertClient: alertCli})

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
		queryErr: &UnknownDBError{DB: "fakedb", AvailableDatabases: []string{"db1", "db2"}},
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
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", KustoClient: kcli, AlertClient: alertCli})

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
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", KustoClient: kcli, AlertClient: alertCli})

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
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", KustoClient: kcli, AlertClient: alertCli})

	// default healthy
	metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name).Set(QueryHealthHealthy)

	w.ExecuteQuery(context.Background())
	gaugeValue := getGaugeValue(t, metrics.QueryHealth.WithLabelValues(rule.Namespace, rule.Name))
	// query should be healthy - notification was throttled.
	require.Equal(t, QueryHealthHealthy, gaugeValue)

	require.Equal(t, createdAlert.Destination, rule.Destination)
	require.Contains(t, createdAlert.Title, "has too many notifications in eastus")
	require.Contains(t, createdAlert.Summary, "throttled by ADX-Mon")
	require.Contains(t, createdAlert.Summary, "Click here to show query")
}

func TestWorker_AlertsThrottledDetails(t *testing.T) {
	for _, throttleAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("throttle_at_%d", throttleAt), func(t *testing.T) {
			rule := &rules.Rule{
				Namespace:   "namespace",
				Name:        "name",
				Database:    "fakedb",
				Destination: "destination/queue",
				Query:       `Example | where Value < 10 | project Title, Severity`,
			}
			titles := []string{"First alert", "Second alert", `Third <alert> & "details"`}
			var executedQuery string
			kcli := &fakeKustoClient{
				queryFn: func(ctx context.Context, qc *QueryContext, handler func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
					executedQuery = qc.Query
					for index, title := range titles {
						row := testRow(
							azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.String)},
							azvalue.Values{azvalue.NewString(title), azvalue.NewString(fmt.Sprint(index + 1))},
						)
						if err := handler(ctx, "https://fakedb.mockcluster.kusto.windows.net", qc, row); err != nil {
							return err, index + 1
						}
					}
					return nil, len(titles)
				},
			}
			var attempts int
			var fallbackAlerts []alert.Alert
			alertCli := &fakeAlerter{
				createFn: func(ctx context.Context, endpoint string, notification alert.Alert) error {
					if notification.Source == "notification-failure/namespace/name" {
						fallbackAlerts = append(fallbackAlerts, notification)
						return nil
					}
					attempts++
					if attempts >= throttleAt {
						return fmt.Errorf("notification rejected: %w", alert.ErrTooManyRequests)
					}
					return nil
				},
			}
			executor := NewExecutor(ExecutorOpts{Region: "eastus", KustoClient: kcli, AlertCli: alertCli})
			worker := executor.newWorker(rule)

			worker.ExecuteQuery(context.Background())

			require.Equal(t, throttleAt, attempts)
			require.Len(t, fallbackAlerts, 1)
			fallback := fallbackAlerts[0]
			require.Equal(t, rule.Destination, fallback.Destination)
			require.Equal(t, 3, fallback.Severity)
			require.Equal(t, "notification-failure/namespace/name", fallback.CorrelationID)
			require.Contains(t, fallback.Summary, "<th>Severity</th><th>Title</th>")
			if throttleAt == 1 {
				require.Contains(t, fallback.Summary, "<td>1</td><td>First alert</td>")
			} else {
				require.NotContains(t, fallback.Summary, "First alert")
			}
			require.Contains(t, fallback.Summary, "<td>2</td><td>Second alert</td>")
			require.Contains(t, fallback.Summary, "<td>3</td><td>Third &lt;alert&gt; &amp; &#34;details&#34;</td>")
			require.NotContains(t, fallback.Summary, "Third <alert>")
			queryDetails, err := KustoQueryLinks("", executedQuery, kcli.Endpoint(rule.Database), rule.Database)
			require.NoError(t, err)
			require.Contains(t, fallback.Summary, queryDetails)
		})
	}
}

func TestWorker_AlertsThrottledOverflow(t *testing.T) {
	for _, deliveryThrottled := range []bool{false, true} {
		t.Run(fmt.Sprintf("delivery_throttled_%t", deliveryThrottled), func(t *testing.T) {
			var overflow ThrottledNotificationsError
			for index := range maxThrottledNotificationDetails + 5 {
				overflow.Add(AlertResult{Title: fmt.Sprintf("Overflow alert %d", index), Severity: 2, Summary: "Not retained"})
			}
			require.Len(t, overflow.Notifications, maxThrottledNotificationDetails)
			require.Empty(t, overflow.Notifications[0].Summary)
			var createdAlert alert.Alert
			var attempts int
			worker := NewWorker(&WorkerConfig{
				Rule: &rules.Rule{Namespace: "namespace", Name: "name", Database: "db", Destination: "destination"},
				KustoClient: &fakeKustoClient{queryFn: func(ctx context.Context, qc *QueryContext, handler func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
					row := testRow(
						azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.Long)},
						azvalue.Values{azvalue.NewString("Rejected alert"), azvalue.NewLong(1)},
					)
					require.NoError(t, handler(ctx, "endpoint", qc, row))
					return fmt.Errorf("notification limit: %w", &overflow), 1
				}},
				HandlerFn: func(context.Context, string, *QueryContext, azquery.Row) error {
					attempts++
					if deliveryThrottled {
						return alert.ErrTooManyRequests
					}
					return nil
				},
				AlertClient: &fakeAlerter{createFn: func(ctx context.Context, endpoint string, notification alert.Alert) error {
					createdAlert = notification
					return nil
				}},
			})

			worker.ExecuteQuery(context.Background())

			require.Equal(t, 1, attempts)
			require.Equal(t, maxThrottledNotificationDetails, strings.Count(createdAlert.Summary, "<tr><td>"))
			require.Contains(t, createdAlert.Summary, "Overflow alert 0")
			require.NotContains(t, createdAlert.Summary, fmt.Sprintf("Overflow alert %d", maxThrottledNotificationDetails))
			if deliveryThrottled {
				require.Contains(t, createdAlert.Summary, "Rejected alert")
				require.NotContains(t, createdAlert.Summary, fmt.Sprintf("Overflow alert %d", maxThrottledNotificationDetails-1))
				require.Contains(t, createdAlert.Summary, "6 additional suppressed alerts are not shown")
			} else {
				require.NotContains(t, createdAlert.Summary, "Rejected alert")
				require.Contains(t, createdAlert.Summary, fmt.Sprintf("Overflow alert %d", maxThrottledNotificationDetails-1))
				require.Contains(t, createdAlert.Summary, "5 additional suppressed alerts are not shown")
			}
		})
	}
}

func TestWorker_AlertsThrottledIncompleteResults(t *testing.T) {
	var createdAlert alert.Alert
	worker := NewWorker(&WorkerConfig{
		Rule: &rules.Rule{Namespace: "namespace", Name: "name", Destination: "destination"},
		KustoClient: &fakeKustoClient{queryFn: func(ctx context.Context, qc *QueryContext, handler func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			row := testRow(
				azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.Long)},
				azvalue.Values{azvalue.NewString("Suppressed alert"), azvalue.NewLong(2)},
			)
			require.NoError(t, handler(ctx, "endpoint", qc, row))
			return handler(ctx, "endpoint", qc, testRow(nil, nil)), 2
		}},
		HandlerFn: func(context.Context, string, *QueryContext, azquery.Row) error {
			return alert.ErrTooManyRequests
		},
		AlertClient: &fakeAlerter{createFn: func(ctx context.Context, endpoint string, notification alert.Alert) error {
			createdAlert = notification
			return nil
		}},
	})

	worker.ExecuteQuery(context.Background())

	require.Contains(t, createdAlert.Summary, "<td>2</td><td>Suppressed alert</td>")
	require.Contains(t, createdAlert.Summary, "this table may be incomplete")
	require.Contains(t, createdAlert.Summary, "Click here to show query")
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
	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", KustoClient: kcli, AlertClient: alertCli})

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

	w.alertCli = alertCli

	// Keep metrics as they were after the first test
	w.ExecuteQuery(context.Background())

	// Verify alert creation was attempted
	require.True(t, createCalled, "Create alert should be called")

	// Verify notification health is now healthy (create succeeded)
	notificationHealthValue = getGaugeValue(t, metrics.NotificationUnhealthy.WithLabelValues(rule.Namespace, rule.Name))
	require.Equal(t, NotificationHealthHealthy, notificationHealthValue)
}

func TestWorker_EvaluationMetrics(t *testing.T) {
	tests := []struct {
		name       string
		queryErr   error
		outcome    string
		alertCalls int
	}{
		{name: "success", outcome: evaluationOutcomeSuccess},
		{name: "service error", queryErr: context.DeadlineExceeded, outcome: evaluationOutcomeServiceError},
		{name: "user error", queryErr: &UnknownDBError{DB: "missing"}, outcome: evaluationOutcomeUserError, alertCalls: 1},
		{name: "notification throttled", queryErr: alert.ErrTooManyRequests, outcome: evaluationOutcomeNotificationThrottled, alertCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &rules.Rule{Namespace: "metrics", Name: tt.name, Destination: "destination"}
			alertCalls := 0
			w := NewWorker(&WorkerConfig{
				Rule:        rule,
				Region:      "eastus",
				KustoClient: &fakeKustoClient{queryErr: tt.queryErr},
				AlertClient: &fakeAlerter{createFn: func(context.Context, string, alert.Alert) error {
					alertCalls++
					return nil
				}},
			})

			outcomeCounter := metrics.AlertRuleEvaluationsTotal.WithLabelValues(tt.outcome)
			counterBefore := getCounterValue(t, outcomeCounter)
			histogramCountBefore := getHistogramCount(t, metrics.AlertRuleEvaluationDurationSeconds)

			w.ExecuteQuery(context.Background())

			require.Equal(t, counterBefore+1, getCounterValue(t, outcomeCounter))
			require.Equal(t, histogramCountBefore+1, getHistogramCount(t, metrics.AlertRuleEvaluationDurationSeconds))
			require.Equal(t, tt.alertCalls, alertCalls)
		})
	}
}

func TestWorker_AlertsGeneratedMetricIncludesPartialSuccess(t *testing.T) {
	rule := &rules.Rule{Namespace: "metrics", Name: "partial-alerts", Destination: "destination"}
	w := NewWorker(&WorkerConfig{
		Rule:   rule,
		Region: "eastus",
		KustoClient: &fakeKustoClient{queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			for range 2 {
				require.NoError(t, fn(ctx, "endpoint", qc, testRow(nil, nil)))
			}
			return alert.ErrTooManyRequests, 2
		}},
		AlertClient: &fakeAlerter{},
		HandlerFn: func(context.Context, string, *QueryContext, azquery.Row) error {
			return nil
		},
	})

	counterBefore := getCounterValue(t, metrics.AlertsGeneratedTotal)

	w.ExecuteQuery(context.Background())

	require.Equal(t, counterBefore+2, getCounterValue(t, metrics.AlertsGeneratedTotal))
}

func TestCalculateNextQueryTime(t *testing.T) {
	now := time.Now()
	interval := 5 * time.Minute

	t.Run("first execution returns immediate", func(t *testing.T) {
		w := NewWorker(&WorkerConfig{Rule: &rules.Rule{Namespace: "ns", Name: "rule", Interval: interval, LastQueryTime: time.Time{}}, Region: "eastus"})
		result := w.calculateNextQueryTime()
		// Should be in the past (immediate execution)
		require.True(t, result.Before(time.Now().Add(1*time.Second)), "expected immediate execution")
	})

	t.Run("scheduled execution returns lastQueryTime+interval", func(t *testing.T) {
		last := now.Add(-10 * time.Minute)
		w := NewWorker(&WorkerConfig{Rule: &rules.Rule{Namespace: "ns", Name: "rule", Interval: interval, LastQueryTime: last}, Region: "eastus"})
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

func getCounterValue(t *testing.T, metric prometheus.Metric) float64 {
	t.Helper()

	metricDTO := &dto.Metric{}
	err := metric.Write(metricDTO)
	require.NoError(t, err)
	return metricDTO.Counter.GetValue()
}

func getHistogramCount(t *testing.T, metric prometheus.Metric) uint64 {
	t.Helper()

	metricDTO := &dto.Metric{}
	err := metric.Write(metricDTO)
	require.NoError(t, err)
	return metricDTO.Histogram.GetSampleCount()
}
