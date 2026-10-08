package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/rules"
	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/metrics"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clocktesting "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Observe entry into the slot select without sleeping or changing context error
// semantics. The slot is held by the test, so cancellation ends the wait.
type observedDoneContext struct {
	context.Context
	once     sync.Once
	observed chan struct{}
}

func (c *observedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

type cancelOnDoneContext struct {
	context.Context
	cancel context.CancelFunc
}

func (c *cancelOnDoneContext) Done() <-chan struct{} {
	c.cancel()
	return c.Context.Done()
}

func TestWorker_SlotAcquisitionCancellationRaceDoesNotStartEvaluation(t *testing.T) {
	w := NewWorker(&WorkerConfig{
		Rule:                 &rules.Rule{Namespace: "lifecycle", Name: t.Name()},
		MaxConcurrentQueries: 1,
		KustoClient: &fakeKustoClient{queryFn: func(context.Context, *QueryContext, func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			t.Error("query must not start when slot acquisition races with cancellation")
			return nil, 0
		}},
	})
	before := getHistogramCount(t, metrics.AlertRuleEvaluationDurationSeconds)
	// Cancellation while select operands are evaluated makes both cases ready.
	// Exercise both possible selections without relying on goroutine timing.
	for range 100 {
		ctx, cancel := context.WithCancel(context.Background())
		result := w.executeQueryAttempt(&cancelOnDoneContext{Context: ctx, cancel: cancel}, nil)
		require.True(t, result.aborted)
		require.ErrorIs(t, result.err, context.Canceled)
		require.Nil(t, result.retry, "no evaluation should start after cancellation")
		w.finishAbortedQuery(result)
		require.Empty(t, w.querySlots)
	}
	require.Equal(t, before, getHistogramCount(t, metrics.AlertRuleEvaluationDurationSeconds))
}

type cancelOnEndpointClient struct {
	*fakeKustoClient
	cancel context.CancelFunc
}

func (c *cancelOnEndpointClient) Endpoint(database string) string {
	c.cancel()
	return c.fakeKustoClient.Endpoint(database)
}

func TestWorker_CancellationImmediatelyBeforeQuery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, retry := newLifecycleRetry(t, ctx)
	assertFinished := expectAbortedEvaluation(t, w, retry, evaluationOutcomeCancelled)
	w.kustoClient = &cancelOnEndpointClient{
		cancel: cancel,
		fakeKustoClient: &fakeKustoClient{queryFn: func(context.Context, *QueryContext, func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			t.Error("query must not start when cancellation occurs during setup")
			return nil, 0
		}},
	}
	result := w.executeQueryAttempt(ctx, retry)
	require.True(t, result.aborted)
	require.Same(t, retry, result.retry)
	require.ErrorIs(t, result.err, context.Canceled)
	w.finishAbortedQuery(result)
	assertFinished()
}

func newLifecycleRetry(t *testing.T, ctx context.Context) (*worker, *queryRetryState) {
	t.Helper()
	w := NewWorker(&WorkerConfig{
		Rule:                 &rules.Rule{Namespace: "lifecycle", Name: t.Name(), Interval: time.Hour},
		Clock:                clocktesting.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		MaxConcurrentQueries: 1,
		KustoClient:          &fakeKustoClient{},
		AlertClient: &fakeAlerter{createFn: func(context.Context, string, alert.Alert) error {
			t.Error("aborted evaluation must not create a failure alert")
			return nil
		}},
	})
	metrics.QueryHealth.WithLabelValues(w.rule.Namespace, w.rule.Name).Set(QueryHealthHealthy)
	metrics.NotificationUnhealthy.WithLabelValues(w.rule.Namespace, w.rule.Name).Set(NotificationHealthHealthy)
	result := w.executeQueryAttempt(ctx, nil)
	require.NoError(t, result.err)
	require.NotNil(t, result.retry)
	t.Cleanup(result.retry.evaluationCancel)
	return w, result.retry
}

func expectAbortedEvaluation(t *testing.T, w *worker, retry *queryRetryState, outcome string) func() {
	t.Helper()
	counter := metrics.AlertRuleEvaluationsTotal.WithLabelValues(outcome)
	before := getCounterValue(t, counter)
	durationsBefore := getHistogramCount(t, metrics.AlertRuleEvaluationDurationSeconds)
	cancel := retry.evaluationCancel
	cancelCalls := 0
	retry.evaluationCancel = func() {
		cancelCalls++
		cancel()
	}
	return func() {
		t.Helper()
		require.Equal(t, before+1, getCounterValue(t, counter), "evaluation must finish exactly once")
		require.Equal(t, durationsBefore+1, getHistogramCount(t, metrics.AlertRuleEvaluationDurationSeconds))
		require.Equal(t, 1, cancelCalls, "evaluation context must be cleaned up exactly once")
		require.True(t, retry.evaluation.finished)
		require.Equal(t, outcome, retry.evaluation.outcome)
		require.Error(t, retry.evaluationContext.Err())
		require.Equal(t, QueryHealthHealthy, getGaugeValue(t, metrics.QueryHealth.WithLabelValues(w.rule.Namespace, w.rule.Name)))
		require.Equal(t, NotificationHealthHealthy, getGaugeValue(t, metrics.NotificationUnhealthy.WithLabelValues(w.rule.Namespace, w.rule.Name)))
	}
}

func receiveAttempt(t *testing.T, done <-chan queryAttemptResult) queryAttemptResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(time.Second):
		t.Fatal("query attempt did not stop cooperatively")
		return queryAttemptResult{}
	}
}

func TestWorker_LifecycleTerminationTakesPrecedence(t *testing.T) {
	for _, parentDeadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancelled", true: "parent deadline"}[parentDeadline], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w, retry := newLifecycleRetry(t, ctx)
			retry.evaluationCancel()
			retry.evaluationContext, retry.evaluationCancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
			defer retry.evaluationCancel()
			outcome, wantErr := evaluationOutcomeCancelled, context.Canceled
			if parentDeadline {
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer cancel()
				outcome, wantErr = evaluationOutcomeServiceError, context.DeadlineExceeded
			} else {
				cancel()
			}
			assertFinished := expectAbortedEvaluation(t, w, retry, outcome)
			result := w.executeQueryAttempt(ctx, retry)
			require.True(t, result.aborted)
			require.ErrorIs(t, result.err, wantErr)
			w.finishAbortedQuery(result)
			assertFinished()
		})
	}
}

func TestWorker_ParentDeadlineCancelsInFlightRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	w, retry := newLifecycleRetry(t, ctx)
	assertFinished := expectAbortedEvaluation(t, w, retry, evaluationOutcomeServiceError)
	w.kustoClient = &fakeKustoClient{queryFn: func(ctx context.Context, _ *QueryContext, _ func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
		<-ctx.Done()
		return ctx.Err(), 0
	}}
	result := w.executeQueryAttempt(ctx, retry)
	require.True(t, result.aborted)
	require.ErrorIs(t, result.err, context.DeadlineExceeded)
	w.finishAbortedQuery(result)
	assertFinished()
}

func TestWorker_ReportingHandoffCancellation(t *testing.T) {
	for _, completion := range []string{"success", "throttled", "setup error"} {
		t.Run(completion, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w, retry := newLifecycleRetry(t, ctx)
			assertFinished := expectAbortedEvaluation(t, w, retry, evaluationOutcomeCancelled)
			result := queryAttemptResult{retry: retry}
			switch completion {
			case "throttled":
				result.err = alert.ErrTooManyRequests
			case "setup error":
				result.err, result.setupError = errors.New("setup failed"), true
			}

			scheme := runtime.NewScheme()
			require.NoError(t, alertrulev1.AddToScheme(scheme))
			cr := &alertrulev1.AlertRule{
				ObjectMeta: metav1.ObjectMeta{Namespace: w.rule.Namespace, Name: w.rule.Name},
				Spec:       alertrulev1.AlertRuleSpec{Database: "TestDB", Query: "Table | take 1", Destination: "owning-team"},
				Status:     alertrulev1.AlertRuleStatus{Status: "Success", Message: "previous evaluation"},
			}
			w.ctrlCli = fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(cr).WithObjects(cr).Build()
			cancel()
			w.handleQueryResult(ctx, result)
			assertFinished()
			updated := &alertrulev1.AlertRule{}
			require.NoError(t, w.ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}, updated))
			require.Equal(t, cr.Status, updated.Status, "shutdown must not persist a terminal CRD error")
		})
	}
}

func TestWorker_ShutdownDuringFailureNotification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"cancelled return", context.Canceled},
		{"nil return", nil},
		{"delivery failure", errors.New("delivery failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w, retry := newLifecycleRetry(t, ctx)
			assertFinished := expectAbortedEvaluation(t, w, retry, evaluationOutcomeCancelled)
			calls := 0
			w.alertCli = &fakeAlerter{createFn: func(notificationCtx context.Context, _ string, _ alert.Alert) error {
				calls++
				cancel()
				require.ErrorIs(t, notificationCtx.Err(), context.Canceled)
				return tc.err
			}}
			w.handleQueryResult(ctx, queryAttemptResult{retry: retry, err: &UnknownDBError{DB: "missing"}})
			require.Equal(t, 1, calls, "an already in-flight alert cannot be retracted")
			assertFinished()
		})
	}
}

func TestWorker_SDKCancelledWithLiveLifecycleIsServiceError(t *testing.T) {
	w := NewWorker(&WorkerConfig{
		Rule:        &rules.Rule{Namespace: "lifecycle", Name: t.Name()},
		KustoClient: &fakeKustoClient{queryErr: context.Canceled},
		AlertClient: &fakeAlerter{createFn: func(context.Context, string, alert.Alert) error {
			t.Error("ordinary SDK cancellation must retain the non-transient error policy")
			return nil
		}},
	})
	result := w.executeQueryAttempt(context.Background(), nil)
	require.False(t, result.aborted)
	require.ErrorIs(t, result.err, context.Canceled)
	w.handleQueryResult(context.Background(), result)
	require.Equal(t, evaluationOutcomeServiceError, result.retry.evaluation.outcome)
	require.Equal(t, QueryHealthUnhealthy, getGaugeValue(t, metrics.QueryHealth.WithLabelValues(w.rule.Namespace, w.rule.Name)))
}
