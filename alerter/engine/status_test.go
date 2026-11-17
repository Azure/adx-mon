package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/rules"
	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/metrics"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	aztypes "github.com/Azure/azure-kusto-go/azkustodata/types"
	azvalue "github.com/Azure/azure-kusto-go/azkustodata/value"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clocktesting "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestWorker_StatusUpdate_NoKubernetesClient(t *testing.T) {
	// A missing Kubernetes client must not prevent the query from executing.
	queryExecuted := false
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			queryExecuted = true
			require.NoError(t, fn(ctx, "fake.endpoint", qc, testRow(nil, nil)))
			return nil, 1
		},
	}

	rule := &rules.Rule{
		Namespace: "test-namespace",
		Name:      "test-rule",
		Database:  "TestDB",
	}

	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", KustoClient: kcli, AlertClient: &fakeAlerter{}, AlertAddr: "http://fake.alert.addr", HandlerFn: func(ctx context.Context, endpoint string, qc *QueryContext, row azquery.Row) error {
		return nil
	}})

	w.ExecuteQuery(context.Background())

	require.True(t, queryExecuted)
}

func TestWorker_AlertsGeneratedMetric(t *testing.T) {
	// Only successful handler calls count as generated alerts.
	tests := []struct {
		name           string
		handlerSuccess bool
		expectedAlerts int
		numRows        int
	}{
		{
			name:           "All alerts succeed",
			handlerSuccess: true,
			expectedAlerts: 3,
			numRows:        3,
		},
		{
			name:           "Some alerts fail",
			handlerSuccess: false,
			expectedAlerts: 0,
			numRows:        2,
		},
		{
			name:           "No rows returned",
			handlerSuccess: true,
			expectedAlerts: 0,
			numRows:        0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kcli := &fakeKustoClient{
				queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
					for i := 0; i < tt.numRows; i++ {
						fn(ctx, "fake.endpoint", qc, testRow(nil, nil))
					}
					return nil, tt.numRows
				},
			}

			rule := &rules.Rule{
				Namespace: "test-namespace",
				Name:      "test-rule",
				Database:  "TestDB",
			}

			w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", KustoClient: kcli, AlertClient: &fakeAlerter{}, AlertAddr: "http://fake.alert.addr", HandlerFn: func(ctx context.Context, endpoint string, qc *QueryContext, row azquery.Row) error {
				if tt.handlerSuccess {
					return nil // Success - alert generated
				}
				return fmt.Errorf("handler failed") // Failure - no alert generated
			}})

			counterBefore := getCounterValue(t, metrics.AlertsGeneratedTotal)
			w.ExecuteQuery(context.Background())

			require.Equal(t, counterBefore+float64(tt.expectedAlerts), getCounterValue(t, metrics.AlertsGeneratedTotal))
		})
	}
}

func TestWorker_StatusUpdateIncludesLatestEvaluationDetails(t *testing.T) {
	// The status update persists the latest duration, row count, and alert count.
	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	alertRule := &alertrulev1.AlertRule{
		Namespace: "test-namespace", Name: "test-rule",
		Spec: alertrulev1.AlertRuleSpec{
			Database:    "TestDB",
			Query:       "Table | take 1",
			Destination: "destination",
		},
	}
	ctrlCli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&alertrulev1.AlertRule{}).
		WithObjects(alertRule).
		Build()

	rule := &rules.Rule{Namespace: alertRule.Namespace, Name: alertRule.Name, Database: "TestDB"}
	w := NewWorker(&WorkerConfig{
		Rule:       rule,
		CtrlClient: ctrlCli,
	})

	fakeClock := clocktesting.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	evaluation := newAlertRuleEvaluation(rule, fakeClock)
	fakeClock.Step(1500 * time.Millisecond)
	require.Equal(t, 1500*time.Millisecond, evaluation.elapsed())
	fakeClock.Step(time.Hour)
	require.Equal(t, 1500*time.Millisecond, evaluation.elapsed(), "elapsed duration should be cached after completion")
	evaluation.rows = 2
	evaluation.alertsGenerated = 2
	w.updateAlertRuleStatus(context.Background(), evaluation, "Success", "")

	updated := &alertrulev1.AlertRule{}
	require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: alertRule.Namespace, Name: alertRule.Name}, updated))
	require.Equal(t, "Success", updated.Status.Status)
	require.Equal(t, int64(1500), updated.Status.LastEvaluationDurationMilliseconds)
	require.Equal(t, int64(2), updated.Status.LastRowsReturned)
	require.Equal(t, int64(2), updated.Status.LastAlertsGenerated)
	require.False(t, updated.Status.LastQueryTime.IsZero())
	require.False(t, updated.Status.LastAlertTime.IsZero())
}

func TestWorker_OverdueExecutionPersistsCurrentWindow(t *testing.T) {
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	interval := 5 * time.Minute
	lastQueryTime := metav1.NewTime(base.Add(-time.Hour))

	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))
	alertRule := &alertrulev1.AlertRule{
		Namespace: "test-namespace", Name: "overdue-rule",
		Spec: alertrulev1.AlertRuleSpec{
			Database:    "TestDB",
			Interval:    metav1.Duration{Duration: interval},
			Query:       "Table | take 1",
			Destination: "destination",
		},
		Status: alertrulev1.AlertRuleStatus{
			LastQueryTime: lastQueryTime,
		},
	}
	ctrlCli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&alertrulev1.AlertRule{}).
		WithObjects(alertRule).
		Build()

	queries := make(chan *QueryContext, 1)
	fakeClock := clocktesting.NewFakeClock(base)
	w := NewWorker(&WorkerConfig{
		Rule: &rules.Rule{
			Namespace:     alertRule.Namespace,
			Name:          alertRule.Name,
			Database:      "TestDB",
			Interval:      interval,
			LastQueryTime: lastQueryTime.Time,
		},
		Clock:      fakeClock,
		CtrlClient: ctrlCli,
		KustoClient: &fakeKustoClient{queryFn: func(_ context.Context, qc *QueryContext, _ func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			queries <- qc
			return nil, 0
		}},
		AlertClient: &fakeAlerter{},
	})

	ctx, cancel := context.WithCancel(context.Background())
	w.Run(ctx)
	waitForWorkerTimer(t, fakeClock)
	fakeClock.Step(0)
	require.Equal(t, base, waitForQuery(t, queries).EndTime)

	require.Eventually(t, func() bool {
		updated := &alertrulev1.AlertRule{}
		if err := ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: alertRule.Namespace, Name: alertRule.Name}, updated); err != nil {
			return false
		}
		return updated.Status.LastQueryTime.Time.Equal(base)
	}, time.Second, time.Millisecond)

	cancel()
	w.Close()
}

func TestWorker_ThrottledStatusIncludesPartialAlerts(t *testing.T) {
	// Alerts generated before throttling must remain visible in AlertRule status.
	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	alertRule := &alertrulev1.AlertRule{
		Namespace: "test-namespace", Name: "throttled-rule",
		Spec: alertrulev1.AlertRuleSpec{
			Database:    "TestDB",
			Query:       "Table | take 2",
			Destination: "destination",
		},
	}
	ctrlCli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&alertrulev1.AlertRule{}).
		WithObjects(alertRule).
		Build()

	w := NewWorker(&WorkerConfig{
		Rule:   &rules.Rule{Namespace: alertRule.Namespace, Name: alertRule.Name, Database: "TestDB", Destination: "destination"},
		Region: "eastus",
		KustoClient: &fakeKustoClient{queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			for range 2 {
				require.NoError(t, fn(ctx, "fake.endpoint", qc, testRow(nil, nil)))
			}
			return alert.ErrTooManyRequests, 2
		}},
		AlertClient: &fakeAlerter{},
		CtrlClient:  ctrlCli,
		HandlerFn: func(context.Context, string, *QueryContext, azquery.Row) error {
			return nil
		},
	})

	w.ExecuteQuery(context.Background())

	updated := &alertrulev1.AlertRule{}
	require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: alertRule.Namespace, Name: alertRule.Name}, updated))
	require.Equal(t, "Throttled", updated.Status.Status)
	require.Equal(t, int64(2), updated.Status.LastRowsReturned)
	require.Equal(t, int64(2), updated.Status.LastAlertsGenerated)
	require.False(t, updated.Status.LastAlertTime.IsZero())
}

func TestWorker_UnmatchedCriteriaNeverClobbersCoLocatedAlerterStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	alertRule := &alertrulev1.AlertRule{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  "monitoring",
			Name:       "colocated-rule",
			UID:        types.UID("uid-colocated-1"),
			Generation: 2,
		},
		Spec: alertrulev1.AlertRuleSpec{
			Database:           "Metrics",
			Interval:           metav1.Duration{Duration: time.Minute},
			Query:              "Cpu | take 1",
			Destinations:       []string{"icm://infra"},
			Criteria:           map[string][]string{"role": {"infra"}},
			CriteriaExpression: "shard == 'a'",
		},
	}
	ctrlCli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&alertrulev1.AlertRule{}).
		WithObjects(alertRule).
		Build()

	fc, err := alert.NewFanoutClient([]string{"http://receiver.example:8080"}, alert.WithStaticCatchAll())
	require.NoError(t, err)
	require.NoError(t, fc.Open(context.Background()))
	defer fc.Close()

	rule := &rules.Rule{
		UID:                     string(alertRule.UID),
		Generation:              alertRule.Generation,
		Namespace:               alertRule.Namespace,
		Name:                    alertRule.Name,
		Database:                "Metrics",
		Interval:                time.Minute,
		Destinations:            []string{"icm://infra"},
		HasExplicitDestinations: true,
		Criteria:                alertRule.Spec.Criteria,
		CriteriaExpression:      alertRule.Spec.CriteriaExpression,
	}

	// 1. Matched alerter evaluates and writes Status + Conditions.
	matchedWorker := NewWorker(&WorkerConfig{
		Rule:   rule,
		Region: "eastus",
		Tags:   map[string]string{"role": "infra", "shard": "a"},
		KustoClient: &fakeKustoClient{queryFn: func(_ context.Context, _ *QueryContext, _ func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			return nil, 0
		}},
		AlertClient: fc,
		CtrlClient:  ctrlCli,
	})
	matchedWorker.ExecuteQuery(context.Background())

	afterMatched := &alertrulev1.AlertRule{}
	require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: rule.Namespace, Name: rule.Name}, afterMatched))
	require.Equal(t, "Success", afterMatched.Status.Status)
	critCond := meta.FindStatusCondition(afterMatched.Status.Conditions, alertrulev1.ConditionCriteria)
	require.NotNil(t, critCond)
	require.Equal(t, metav1.ConditionTrue, critCond.Status)

	// 2. Co-located alerters with non-matching criteria OR missing CEL tag key must NOT write Status at all!
	for _, tags := range []map[string]string{
		{"role": "tenant", "shard": "b"}, // criteria false
		{"role": "infra"},                // CEL missing identifier 'shard' -> matchErr != nil
	} {
		unmatchedWorker := NewWorker(&WorkerConfig{
			Rule:   rule,
			Region: "eastus",
			Tags:   tags,
			KustoClient: &fakeKustoClient{queryFn: func(_ context.Context, _ *QueryContext, _ func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
				return nil, 0
			}},
			AlertClient: fc,
			CtrlClient:  ctrlCli,
		})
		ctx, cancel := context.WithCancel(context.Background())
		unmatchedWorker.Run(ctx)
		unmatchedWorker.ExecuteQuery(ctx)
		cancel()
		unmatchedWorker.Close()
	}

	afterUnmatched := &alertrulev1.AlertRule{}
	require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: rule.Namespace, Name: rule.Name}, afterUnmatched))
	require.Equal(t, afterMatched.ResourceVersion, afterUnmatched.ResourceVersion, "unmatched workers must never issue Status().Update")
	critAfter := meta.FindStatusCondition(afterUnmatched.Status.Conditions, alertrulev1.ConditionCriteria)
	require.NotNil(t, critAfter)
	require.Equal(t, metav1.ConditionTrue, critAfter.Status)
}

func TestWorker_MultiValuedConditionsLatchAcrossZeroRowEvalsAndRecoverOn201OrGenBump(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	var failEndpointB atomic.Bool
	failEndpointB.Store(true)

	srvSelectiveA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"const":"icm://team-a"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srvSelectiveA.Close()

	postFailedCh := make(chan struct{}, 4)
	postRecoveredCh := make(chan struct{}, 4)
	srvSelectiveB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"const":"pagerduty://team-b"}`))
			return
		}
		if failEndpointB.Load() {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("receiver rejected payload for secret-path"))
			select {
			case postFailedCh <- struct{}{}:
			default:
			}
			return
		}
		w.WriteHeader(http.StatusCreated)
		select {
		case postRecoveredCh <- struct{}{}:
		default:
		}
	}))
	defer srvSelectiveB.Close()

	rawURLA := srvSelectiveA.URL + "/secret-a?sig=SUPER_SECRET_TOKEN_A"
	rawURLB := srvSelectiveB.URL + "/secret-b?sig=SUPER_SECRET_TOKEN_B"
	fc, err := alert.NewFanoutClient([]string{rawURLA, rawURLB}, alert.WithConcurrency(1))
	require.NoError(t, err)
	require.NoError(t, fc.Open(context.Background()))
	defer fc.Close()

	epIDB := fc.EndpointIDs()[1]

	alertRule := &alertrulev1.AlertRule{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  "monitoring",
			Name:       "latched-rule",
			UID:        types.UID("uid-latch-1"),
			Generation: 1,
		},
		Spec: alertrulev1.AlertRuleSpec{
			Database:     "Metrics",
			Interval:     metav1.Duration{Duration: time.Minute},
			Query:        "Cpu | take 1",
			Destinations: []string{"icm://team-a", "pagerduty://team-b"},
		},
	}
	ctrlCli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&alertrulev1.AlertRule{}).
		WithObjects(alertRule).
		Build()

	var emitRows atomic.Int32
	emitRows.Store(1)
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, handler func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			n := int(emitRows.Load())
			cols := azquery.Columns{
				testColumn(0, "Title", aztypes.String),
				testColumn(1, "Severity", aztypes.Long),
			}
			for range n {
				row := testRow(cols, azvalue.Values{azvalue.NewString("CPU Hot"), azvalue.NewLong(2)})
				if err := handler(ctx, "https://metrics.kusto.windows.net", qc, row); err != nil {
					return err, n
				}
			}
			return nil, n
		},
	}

	makeExec := func(gen int64, uid string, dests []string) *Executor {
		r := &rules.Rule{
			UID:                     uid,
			Generation:              gen,
			Version:                 fmt.Sprintf("%d", gen),
			Namespace:               alertRule.Namespace,
			Name:                    alertRule.Name,
			Database:                "Metrics",
			Interval:                time.Minute,
			Query:                   "Cpu | take 1",
			Destinations:            dests,
			HasExplicitDestinations: true,
		}
		return NewExecutor(ExecutorOpts{
			AlertCli:    fc,
			KustoClient: kcli,
			RuleStore:   &sliceRuleStore{rules: []*rules.Rule{r}},
			Region:      "eastus",
			CtrlCli:     ctrlCli,
			Concurrency: 1,
		})
	}

	gen1Meta := alert.RuleMeta{
		UID:        "uid-latch-1",
		Namespace:  alertRule.Namespace,
		Name:       alertRule.Name,
		Generation: 1,
	}

	// Evaluation 1 (1 row): Endpoint A succeeds (201), Endpoint B fails asynchronously (400).
	makeExec(1, "uid-latch-1", []string{"icm://team-a", "pagerduty://team-b"}).RunOnce(context.Background())
	select {
	case <-postFailedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for async failure on endpoint B")
	}
	require.Eventually(t, func() bool {
		return fc.EvaluateNotificationDelivery(gen1Meta).Status == metav1.ConditionFalse
	}, 3*time.Second, time.Millisecond)

	// Evaluation 2 & 3 (0 rows!): latched failure on Endpoint B must stay visible in ConditionNotificationDelivery!
	emitRows.Store(0)
	for range 2 {
		makeExec(1, "uid-latch-1", []string{"icm://team-a", "pagerduty://team-b"}).RunOnce(context.Background())

		got := &alertrulev1.AlertRule{}
		require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: alertRule.Namespace, Name: alertRule.Name}, got))

		destCond := meta.FindStatusCondition(got.Status.Conditions, alertrulev1.ConditionDestinationMatched)
		require.NotNil(t, destCond)
		require.Equal(t, metav1.ConditionTrue, destCond.Status)

		delivCond := meta.FindStatusCondition(got.Status.Conditions, alertrulev1.ConditionNotificationDelivery)
		require.NotNil(t, delivCond)
		require.Equal(t, metav1.ConditionFalse, delivCond.Status)
		require.Equal(t, alertrulev1.ReasonDeliveryFailed, delivCond.Reason)
		require.Contains(t, delivCond.Message, epIDB)
		require.Contains(t, delivCond.Message, "HTTP 400 Bad Request")
		for _, secret := range []string{rawURLA, rawURLB, "secret-a", "secret-b", "SUPER_SECRET_TOKEN_A", "SUPER_SECRET_TOKEN_B"} {
			require.NotContains(t, destCond.Message, secret)
			require.NotContains(t, delivCond.Message, secret)
		}
	}

	// Recovery Path 1: Endpoint B recovers and returns 201 Created on Evaluation 4 (1 row), followed by Evaluation 5 (0 rows)!
	failEndpointB.Store(false)
	emitRows.Store(1)
	makeExec(1, "uid-latch-1", []string{"icm://team-a", "pagerduty://team-b"}).RunOnce(context.Background())
	select {
	case <-postRecoveredCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for async 201 recovery on endpoint B")
	}
	require.Eventually(t, func() bool {
		return fc.EvaluateNotificationDelivery(gen1Meta).Status == metav1.ConditionTrue
	}, 3*time.Second, time.Millisecond)

	emitRows.Store(0)
	makeExec(1, "uid-latch-1", []string{"icm://team-a", "pagerduty://team-b"}).RunOnce(context.Background())
	recovered := &alertrulev1.AlertRule{}
	require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: alertRule.Namespace, Name: alertRule.Name}, recovered))
	delivAfter201 := meta.FindStatusCondition(recovered.Status.Conditions, alertrulev1.ConditionNotificationDelivery)
	require.NotNil(t, delivAfter201)
	require.Equal(t, metav1.ConditionTrue, delivAfter201.Status)

	// Recovery Path 2: Fail again on generation 1, then bump metadata.generation to 2 (0 rows) -> clears latch immediately!
	failEndpointB.Store(true)
	emitRows.Store(1)
	makeExec(1, "uid-latch-1", []string{"icm://team-a", "pagerduty://team-b"}).RunOnce(context.Background())
	select {
	case <-postFailedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for second async failure on endpoint B")
	}
	require.Eventually(t, func() bool {
		return fc.EvaluateNotificationDelivery(gen1Meta).Status == metav1.ConditionFalse
	}, 3*time.Second, time.Millisecond)

	// Bump generation to 2 and change destinations to an unrouted target to also verify DestinationMatched=False!
	require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: alertRule.Namespace, Name: alertRule.Name}, recovered))
	recovered.Generation = 2
	recovered.Spec.Destinations = []string{"slack://no-plugin"}
	require.NoError(t, ctrlCli.Update(context.Background(), recovered))

	emitRows.Store(0)
	makeExec(2, "uid-latch-1", []string{"slack://no-plugin"}).RunOnce(context.Background())
	afterGenBump := &alertrulev1.AlertRule{}
	require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: alertRule.Namespace, Name: alertRule.Name}, afterGenBump))

	delivGen2 := meta.FindStatusCondition(afterGenBump.Status.Conditions, alertrulev1.ConditionNotificationDelivery)
	require.NotNil(t, delivGen2)
	require.Equal(t, metav1.ConditionTrue, delivGen2.Status, "generation bump must clear stale generation's latched delivery failure")
	require.Equal(t, int64(2), delivGen2.ObservedGeneration)

	destGen2 := meta.FindStatusCondition(afterGenBump.Status.Conditions, alertrulev1.ConditionDestinationMatched)
	require.NotNil(t, destGen2)
	require.Equal(t, metav1.ConditionFalse, destGen2.Status)
	require.Equal(t, alertrulev1.ReasonNoMatchingEndpoint, destGen2.Reason)
}

func TestWorker_MetaAlertsPopulateDestinationsAndStatusMessagesCapAt512SanitizedUTF8(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	var receivedMu sync.Mutex
	var receivedIcM []alert.Alert
	var receivedSlack []alert.Alert

	srvIcM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"pattern":"^icm://"}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		var a alert.Alert
		require.NoError(t, json.Unmarshal(b, &a))
		if a.Destination == "" {
			// Simulate strict receiver rejecting empty Destination with 400!
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		receivedMu.Lock()
		receivedIcM = append(receivedIcM, a)
		receivedMu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srvIcM.Close()

	srvSlack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"pattern":"^slack://"}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		var a alert.Alert
		_ = json.Unmarshal(b, &a)
		receivedMu.Lock()
		receivedSlack = append(receivedSlack, a)
		receivedMu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srvSlack.Close()

	rawIcMURL := srvIcM.URL + "/webhook-path?secret=WEBHOOK_SECRET_999"
	fc, err := alert.NewFanoutClient([]string{rawIcMURL, srvSlack.URL}, alert.WithConcurrency(1))
	require.NoError(t, err)
	require.NoError(t, fc.Open(context.Background()))

	alertRule := &alertrulev1.AlertRule{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  "monitoring",
			Name:       "meta-alert-rule",
			UID:        types.UID("uid-meta-1"),
			Generation: 1,
		},
		Spec: alertrulev1.AlertRuleSpec{
			Database:     "Metrics",
			Interval:     metav1.Duration{Duration: time.Minute},
			Query:        "BadQuery",
			Destinations: []string{"pagerduty://ops", "icm://compute-oncall"},
		},
	}
	ctrlCli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&alertrulev1.AlertRule{}).
		WithObjects(alertRule).
		Build()

	// Trigger a user validation error containing a raw webhook URL + >600 bytes of multi-byte UTF-8 ("é")!
	longErrText := "validation error near " + rawIcMURL + " " + strings.Repeat("é", 400)
	userErr := &NotificationValidationError{Msg: longErrText}

	kcli := &fakeKustoClient{
		queryFn: func(_ context.Context, _ *QueryContext, _ func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			return userErr, 0
		},
	}

	rule := &rules.Rule{
		UID:                     "uid-meta-1",
		Generation:              1,
		Version:                 "1",
		Namespace:               alertRule.Namespace,
		Name:                    alertRule.Name,
		Database:                "Metrics",
		Interval:                time.Minute,
		Query:                   "BadQuery",
		Destinations:            []string{"pagerduty://ops", "icm://compute-oncall"},
		HasExplicitDestinations: true,
	}
	exec := NewExecutor(ExecutorOpts{
		AlertCli:    fc,
		KustoClient: kcli,
		RuleStore:   &sliceRuleStore{rules: []*rules.Rule{rule}},
		Region:      "eastus",
		CtrlCli:     ctrlCli,
		Concurrency: 1,
	})
	exec.RunOnce(context.Background())
	require.NoError(t, fc.Close())

	// 1. alert-failure/* meta-alert resolved Destination="icm://compute-oncall" & Destinations=["icm://compute-oncall"] on srvIcM,
	// and was NOT sent to unmatched srvSlack!
	receivedMu.Lock()
	require.Len(t, receivedIcM, 1)
	require.Contains(t, receivedIcM[0].CorrelationID, "alert-failure/")
	require.Equal(t, "icm://compute-oncall", receivedIcM[0].Destination)
	require.Equal(t, []string{"icm://compute-oncall"}, receivedIcM[0].Destinations)
	require.Empty(t, receivedSlack)
	receivedMu.Unlock()

	// 2. Status.Message and all Conditions[*].Message are valid UTF-8, <= 512 bytes, and redact raw webhook URLs/secrets!
	updated := &alertrulev1.AlertRule{}
	require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: alertRule.Namespace, Name: alertRule.Name}, updated))
	require.LessOrEqual(t, len(updated.Status.Message), alert.MaxStatusMessageLength)
	require.True(t, utf8.ValidString(updated.Status.Message))
	require.Contains(t, updated.Status.Message, alert.RedactURL(rawIcMURL))
	require.NotContains(t, updated.Status.Message, "webhook-path")
	require.NotContains(t, updated.Status.Message, "WEBHOOK_SECRET_999")
	for _, cond := range updated.Status.Conditions {
		require.LessOrEqual(t, len(cond.Message), alert.MaxStatusMessageLength)
		require.True(t, utf8.ValidString(cond.Message))
		require.NotContains(t, cond.Message, "webhook-path")
		require.NotContains(t, cond.Message, "WEBHOOK_SECRET_999")
	}
}

func TestWorker_NotificationFailureSummaryMetaAlertRoutesMatchedDestinationsOnly(t *testing.T) {
	var mu sync.Mutex
	var icmPosts []alert.Alert
	var slackPosts []alert.Alert

	srvIcM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"pattern":"^icm://"}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		var a alert.Alert
		require.NoError(t, json.Unmarshal(b, &a))
		if a.Destination == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		icmPosts = append(icmPosts, a)
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srvIcM.Close()

	srvSlack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"pattern":"^slack://"}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		var a alert.Alert
		_ = json.Unmarshal(b, &a)
		mu.Lock()
		slackPosts = append(slackPosts, a)
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srvSlack.Close()

	fc, err := alert.NewFanoutClient([]string{srvIcM.URL, srvSlack.URL}, alert.WithConcurrency(1))
	require.NoError(t, err)
	require.NoError(t, fc.Open(context.Background()))

	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, handler func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			cols := azquery.Columns{
				testColumn(0, "Title", aztypes.String),
				testColumn(1, "Severity", aztypes.Long),
			}
			row := testRow(cols, azvalue.Values{azvalue.NewString("Row 1"), azvalue.NewLong(2)})
			require.NoError(t, handler(ctx, "https://metrics.kusto.windows.net", qc, row))
			var overflow ThrottledNotificationsError
			overflow.Add(AlertResult{Title: "Overflow Row", Severity: 2})
			return fmt.Errorf("row cap exceeded: %w", &overflow), 2
		},
	}

	rule := &rules.Rule{
		UID:                     "uid-summary-meta",
		Generation:              1,
		Version:                 "1",
		Namespace:               "monitoring",
		Name:                    "summary-meta-rule",
		Database:                "Metrics",
		Interval:                time.Minute,
		Query:                   "Cpu | take 10",
		Destinations:            []string{"pagerduty://ops", "icm://compute"},
		HasExplicitDestinations: true,
	}
	exec := NewExecutor(ExecutorOpts{
		AlertCli:    fc,
		KustoClient: kcli,
		RuleStore:   &sliceRuleStore{rules: []*rules.Rule{rule}},
		Region:      "eastus",
		Concurrency: 1,
	})
	exec.RunOnce(context.Background())
	require.NoError(t, fc.Close())

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, icmPosts, 2, "matched selective endpoint receives 1 row + 1 notification-failure summary")
	require.Equal(t, "icm://compute", icmPosts[1].Destination)
	require.Equal(t, []string{"icm://compute"}, icmPosts[1].Destinations)
	require.Equal(t, "notification-failure/monitoring/summary-meta-rule", icmPosts[1].CorrelationID)
	require.Empty(t, slackPosts, "unmatched selective endpoint must not receive row or notification-failure summary")
}

func TestWorker_StaleGenerationOrUIDWorkerSkipsStatusWrite(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	alertRule := &alertrulev1.AlertRule{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  "monitoring",
			Name:       "gen-guard-rule",
			UID:        types.UID("uid-gen-1"),
			Generation: 1,
		},
		Spec: alertrulev1.AlertRuleSpec{
			Database:     "Metrics",
			Interval:     metav1.Duration{Duration: time.Minute},
			Query:        "Cpu | take 1",
			Destinations: []string{"icm://infra"},
		},
	}
	ctrlCli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&alertrulev1.AlertRule{}).
		WithObjects(alertRule).
		Build()

	fc, err := alert.NewFanoutClient([]string{"http://receiver.example:8080"}, alert.WithStaticCatchAll())
	require.NoError(t, err)
	require.NoError(t, fc.Open(context.Background()))
	defer fc.Close()

	gen1Rule := &rules.Rule{
		UID:                     "uid-gen-1",
		Generation:              1,
		Version:                 "1",
		Namespace:               alertRule.Namespace,
		Name:                    alertRule.Name,
		Database:                "Metrics",
		Interval:                time.Minute,
		Query:                   "Cpu | take 1",
		Destinations:            []string{"icm://infra"},
		HasExplicitDestinations: true,
	}
	wGen1 := NewWorker(&WorkerConfig{
		Rule:   gen1Rule,
		Region: "eastus",
		KustoClient: &fakeKustoClient{queryFn: func(_ context.Context, _ *QueryContext, _ func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			return nil, 0
		}},
		AlertClient: fc,
		CtrlClient:  ctrlCli,
	})

	// Generation 1 evaluation writes ObservedGeneration=1 conditions.
	wGen1.ExecuteQuery(context.Background())
	current := &alertrulev1.AlertRule{}
	require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: alertRule.Namespace, Name: alertRule.Name}, current))
	critGen1 := meta.FindStatusCondition(current.Status.Conditions, alertrulev1.ConditionCriteria)
	require.NotNil(t, critGen1)
	require.Equal(t, int64(1), critGen1.ObservedGeneration)

	// User updates CR to Generation 2 (changing criteria/destinations before 1m rule reload).
	current.Generation = 2
	current.Spec.CriteriaExpression = "shard == 'unmatched'"
	current.Spec.Destinations = []string{"slack://unmatched"}
	require.NoError(t, ctrlCli.Update(context.Background(), current))
	rvAfterSpecBump := current.ResourceVersion

	// Stale Generation-1 worker evaluates again before reload: must NOT stamp ObservedGeneration=2!
	wGen1.ExecuteQuery(context.Background())
	afterStaleGen := &alertrulev1.AlertRule{}
	require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: alertRule.Namespace, Name: alertRule.Name}, afterStaleGen))
	require.Equal(t, rvAfterSpecBump, afterStaleGen.ResourceVersion, "stale generation worker must skip Status().Update")
	critStillGen1 := meta.FindStatusCondition(afterStaleGen.Status.Conditions, alertrulev1.ConditionCriteria)
	require.NotNil(t, critStillGen1)
	require.Equal(t, int64(1), critStillGen1.ObservedGeneration)

	// Delete and recreate CR with same namespace/name at Generation 1 but a new UID: old UID worker must also skip!
	require.NoError(t, ctrlCli.Delete(context.Background(), afterStaleGen))
	recreated := &alertrulev1.AlertRule{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  "monitoring",
			Name:       "gen-guard-rule",
			UID:        types.UID("uid-gen-2"),
			Generation: 1,
		},
		Spec: alertRule.Spec,
	}
	require.NoError(t, ctrlCli.Create(context.Background(), recreated))
	rvRecreated := recreated.ResourceVersion

	wGen1.ExecuteQuery(context.Background())
	afterStaleUID := &alertrulev1.AlertRule{}
	require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: alertRule.Namespace, Name: alertRule.Name}, afterStaleUID))
	require.Equal(t, rvRecreated, afterStaleUID.ResourceVersion, "stale UID worker must skip Status().Update")
	require.Empty(t, afterStaleUID.Status.Conditions)
}

type mutableRuleStore struct {
	mu    sync.RWMutex
	rules []*rules.Rule
}

func (s *mutableRuleStore) Rules() []*rules.Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*rules.Rule, len(s.rules))
	copy(out, s.rules)
	return out
}

func (s *mutableRuleStore) SetRules(next []*rules.Rule) {
	s.mu.Lock()
	s.rules = next
	s.mu.Unlock()
}

func TestExecutor_SyncWorkersPrunesDeletedAndReplacedRuleLatchesAndIgnoresLateAsyncOutcomes(t *testing.T) {
	inFlightEntered := make(chan struct{})
	releaseLateFailure := make(chan struct{})
	lateHandlerDone := make(chan struct{})
	markInFlight := sync.OnceFunc(func() { close(inFlightEntered) })
	markLateDone := sync.OnceFunc(func() { close(lateHandlerDone) })
	release := sync.OnceFunc(func() { close(releaseLateFailure) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var a alert.Alert
		_ = json.Unmarshal(b, &a)
		if a.Title == "slow-inflight-old-uid" {
			markInFlight()
			select {
			case <-releaseLateFailure:
			case <-r.Context().Done():
			}
			w.WriteHeader(http.StatusBadRequest)
			markLateDone()
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	fc, err := alert.NewFanoutClient([]string{srv.URL}, alert.WithConcurrency(2))
	require.NoError(t, err)
	epID := fc.EndpointIDs()[0]
	beforePerm := getCounterValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonPermanentError))
	require.NoError(t, fc.Open(context.Background()))
	t.Cleanup(func() { _ = fc.Close() })

	ruleDelete := &rules.Rule{
		UID:         "uid-delete-1",
		Generation:  1,
		Version:     "1",
		Namespace:   "monitoring",
		Name:        "deleted-rule",
		Database:    "Metrics",
		Interval:    time.Hour,
		Query:       "Cpu | take 1",
		Destination: "icm://ops",
	}
	ruleReplaceOld := &rules.Rule{
		UID:         "uid-replace-old",
		Generation:  1,
		Version:     "1",
		Namespace:   "monitoring",
		Name:        "recreated-rule",
		Database:    "Metrics",
		Interval:    time.Hour,
		Query:       "Cpu | take 1",
		Destination: "icm://ops",
	}

	store := &mutableRuleStore{rules: []*rules.Rule{ruleDelete, ruleReplaceOld}}
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, handler func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			cols := azquery.Columns{
				testColumn(0, "Title", aztypes.String),
				testColumn(1, "Severity", aztypes.Long),
			}
			// Emit one immediate 400 failure for every rule, plus one blocked in-flight POST for uid-replace-old.
			if err := handler(ctx, "https://metrics.kusto.windows.net", qc, testRow(cols, azvalue.Values{
				azvalue.NewString("immediate-fail-" + qc.Rule.UID),
				azvalue.NewLong(2),
			})); err != nil {
				return err, 1
			}
			if qc.Rule.UID == "uid-replace-old" {
				if err := handler(ctx, "https://metrics.kusto.windows.net", qc, testRow(cols, azvalue.Values{
					azvalue.NewString("slow-inflight-old-uid"),
					azvalue.NewLong(2),
				})); err != nil {
					return err, 2
				}
				return nil, 2
			}
			return nil, 1
		},
	}

	exec := NewExecutor(ExecutorOpts{
		AlertCli:    fc,
		KustoClient: kcli,
		RuleStore:   store,
		Region:      "eastus",
		Concurrency: 2,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = exec.Close()
	})
	t.Cleanup(release)

	exec.syncWorkers(ctx)

	// Wait until both rules have latched their immediate 400 failures AND uid-replace-old's second POST is in-flight.
	select {
	case <-inFlightEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for slow in-flight POST on uid-replace-old")
	}
	require.Eventually(t, func() bool {
		latched, unhealthy := fc.RetainedHealthRuleCounts()
		return latched == 2 && unhealthy == 2
	}, 3*time.Second, time.Millisecond)

	// Now delete ruleDelete completely and replace ruleReplaceOld with a recreated CR (same namespace/name, new UID, future LastQueryTime so it stays idle).
	ruleReplaceNew := &rules.Rule{
		UID:           "uid-replace-new",
		Generation:    1,
		Version:       "1",
		Namespace:     "monitoring",
		Name:          "recreated-rule",
		Database:      "Metrics",
		Interval:      time.Hour,
		Query:         "Cpu | take 1",
		Destination:   "icm://ops",
		LastQueryTime: time.Now().UTC(),
	}
	store.SetRules([]*rules.Rule{ruleReplaceNew})
	exec.syncWorkers(ctx)

	// Both dead UIDs (uid-delete-1 and uid-replace-old) must be pruned immediately!
	latchedAfterSync, unhealthyAfterSync := fc.RetainedHealthRuleCounts()
	require.Zero(t, latchedAfterSync, "syncWorkers must prune deleted and replaced rule UIDs from latchedFailures")
	require.Zero(t, unhealthyAfterSync, "syncWorkers must prune deleted and replaced rule UIDs from unhealthy")

	// Release the late in-flight 400 response from uid-replace-old AFTER its worker was replaced:
	// recordFailure must ignore the dead UID rather than re-inserting it or tainting uid-replace-new!
	release()
	select {
	case <-lateHandlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for late handler completion")
	}
	require.Eventually(t, func() bool {
		return getCounterValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(epID, alert.DropReasonPermanentError)) == beforePerm+3
	}, 3*time.Second, time.Millisecond)
	cancel()
	require.NoError(t, exec.Close())
	require.NoError(t, fc.Close())

	latchedAfterLate, unhealthyAfterLate := fc.RetainedHealthRuleCounts()
	require.Zero(t, latchedAfterLate, "late async failure from removed UID must not re-insert into latchedFailures")
	require.Zero(t, unhealthyAfterLate, "late async failure from removed UID must not taint unhealthy map")
	require.Equal(t, metav1.ConditionTrue, fc.EvaluateNotificationDelivery(alert.RuleMeta{
		UID:        "uid-replace-new",
		Namespace:  "monitoring",
		Name:       "recreated-rule",
		Generation: 1,
	}).Status)
}
