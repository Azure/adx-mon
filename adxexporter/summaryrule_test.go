package adxexporter

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/kustoutil"
	"github.com/Azure/azure-kusto-go/kusto"
	kustoerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/Azure/azure-kusto-go/kusto/data/types"
	"github.com/Azure/azure-kusto-go/kusto/data/value"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	klock "k8s.io/utils/clock/testing"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// ensureTestVFlagSetT sets the -test.v flag required for Azure Kusto SDK mock diagnostics.
func ensureTestVFlagSetT(t *testing.T) {
	t.Helper()
	if flag.Lookup("test.v") == nil {
		flag.String("test.v", "", "")
		err := flag.CommandLine.Set("test.v", "true")
		require.NoError(t, err)
	}
}

// buildMockRows constructs a kusto.MockRows and populates it with provided rows.
func buildMockRows(t *testing.T, cols table.Columns, rows []value.Values) *kusto.MockRows {
	t.Helper()
	mockRows, err := kusto.NewMockRows(cols)
	require.NoError(t, err)
	for _, r := range rows {
		require.NoError(t, mockRows.Row(r))
	}
	return mockRows
}

// buildIteratorFromMockRows creates a RowIterator from pre-built mock rows.
func buildIteratorFromMockRows(t *testing.T, mockRows *kusto.MockRows) *kusto.RowIterator {
	t.Helper()
	ensureTestVFlagSetT(t)
	iter := &kusto.RowIterator{}
	require.NoError(t, iter.Mock(mockRows))
	return iter
}

// createRowIteratorFromMockRows is a convenience wrapper combining buildMockRows + buildIteratorFromMockRows.
// Kept for backwards compatibility with existing tests while providing finer-grained helpers for new cases.
func createRowIteratorFromMockRows(t *testing.T, cols table.Columns, rows []value.Values) *kusto.RowIterator {
	t.Helper()
	return buildIteratorFromMockRows(t, buildMockRows(t, cols, rows))
}

func newFakeClientWithRule(t *testing.T, rule *adxmonv1.SummaryRule) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&adxmonv1.SummaryRule{}).WithObjects(rule).Build()
}

func newBaseReconciler(t *testing.T, c client.Client, mock *MockKustoExecutor, clockNow time.Time) *SummaryRuleReconciler {
	t.Helper()
	return &SummaryRuleReconciler{
		Client:         c,
		Scheme:         c.Scheme(),
		ClusterLabels:  map[string]string{},
		KustoClusters:  map[string]string{"testdb": "https://test"},
		KustoExecutors: map[string]KustoExecutor{"testdb": mock},
		Clock:          klock.NewFakeClock(clockNow),
	}
}

func TestSummaryRule_SubmissionSuccess(t *testing.T) {
	ensureTestVFlagSetT(t)

	rule := &adxmonv1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr", Annotations: map[string]string{adxmonv1.SummaryRuleOwnerAnnotation: adxmonv1.SummaryRuleOwnerADXExporter}},
		Spec: adxmonv1.SummaryRuleSpec{
			Database: "testdb",
			Table:    "TestTable",
			Interval: metav1.Duration{Duration: time.Hour},
			Body:     "TestBody",
		},
	}
	c := newFakeClientWithRule(t, rule)

	mock := NewMockKustoExecutor(t, "testdb", "https://test")
	// First Mgmt: submission returns operation id as a single-cell row
	opCols := table.Columns{{Name: "OperationId", Type: types.String}}
	opRows := []value.Values{{value.String{Value: "operation-id-123", Valid: true}}}
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, opCols, opRows))
	// Second Mgmt (getOperation): return InProgress, ShouldRetry=0
	cols := table.Columns{
		{Name: "LastUpdatedOn", Type: types.DateTime},
		{Name: "OperationId", Type: types.String},
		{Name: "State", Type: types.String},
		{Name: "ShouldRetry", Type: types.Real},
		{Name: "Status", Type: types.String},
	}
	rows := []value.Values{{
		value.DateTime{Value: time.Now(), Valid: true},
		value.String{Value: "operation-id-123", Valid: true},
		value.String{Value: "InProgress", Valid: true},
		value.Real{Value: 0, Valid: true},
		value.String{Value: "", Valid: true},
	}}
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, cols, rows))

	r := newBaseReconciler(t, c, mock, time.Now())

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(rule)})
	require.NoError(t, err)

	// Re-fetch and assert
	var updated adxmonv1.SummaryRule
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(rule), &updated))

	ops := updated.GetAsyncOperations()
	require.Len(t, ops, 1)
	require.Equal(t, "operation-id-123", ops[0].OperationId)

	cond := updated.GetCondition()
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
}

func TestSummaryRule_SubmissionFailure(t *testing.T) {
	ensureTestVFlagSetT(t)

	rule := &adxmonv1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-fail", Annotations: map[string]string{adxmonv1.SummaryRuleOwnerAnnotation: adxmonv1.SummaryRuleOwnerADXExporter}},
		Spec:       adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body"},
	}
	c := newFakeClientWithRule(t, rule)
	mock := NewMockKustoExecutor(t, "testdb", "https://test")
	// First error: initial submission fails
	mock.SetNextError(context.DeadlineExceeded)
	// Second error: backlog resubmission attempt during same reconcile also fails
	mock.SetNextError(context.DeadlineExceeded)
	r := newBaseReconciler(t, c, mock, time.Now())

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(rule)})
	require.NoError(t, err)

	var updated adxmonv1.SummaryRule
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(rule), &updated))

	// Backlog entry recorded with empty OperationId
	ops := updated.GetAsyncOperations()
	require.Len(t, ops, 1)
	require.Equal(t, "", ops[0].OperationId)

	cond := updated.GetCondition()
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
}

func TestSummaryRule_CompletedFailedRemovesAndSetsStatus(t *testing.T) {
	ensureTestVFlagSetT(t)

	now := time.Now().UTC()
	rule := &adxmonv1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-failed-op", Annotations: map[string]string{adxmonv1.SummaryRuleOwnerAnnotation: adxmonv1.SummaryRuleOwnerADXExporter}},
		Spec:       adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body"},
		Status:     adxmonv1.SummaryRuleStatus{},
	}
	// Seed with an existing async operation to track
	rule.SetAsyncOperation(adxmonv1.AsyncOperation{OperationId: "failed-op-1", StartTime: now.Add(-time.Hour).Format(time.RFC3339Nano), EndTime: now.Format(time.RFC3339Nano)})
	// Prevent new submission in this test: mark last exec as now
	rule.SetLastExecutionTime(now)

	c := newFakeClientWithRule(t, rule)
	mock := NewMockKustoExecutor(t, "testdb", "https://test")

	// getOperation returns Failed
	cols := table.Columns{{Name: "LastUpdatedOn", Type: types.DateTime}, {Name: "OperationId", Type: types.String}, {Name: "State", Type: types.String}, {Name: "ShouldRetry", Type: types.Real}, {Name: "Status", Type: types.String}}
	rows := []value.Values{{
		value.DateTime{Value: now, Valid: true},
		value.String{Value: "failed-op-1", Valid: true},
		value.String{Value: "Failed", Valid: true},
		value.Real{Value: 0, Valid: true},
		value.String{Value: "boom", Valid: true},
	}}
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, cols, rows))

	r := newBaseReconciler(t, c, mock, now)
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(rule)})
	require.NoError(t, err)

	var updated adxmonv1.SummaryRule
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(rule), &updated))
	require.Len(t, updated.GetAsyncOperations(), 0, "completed op should be removed")
	cond := updated.GetCondition()
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
}

func TestSummaryRule_RetryOperation(t *testing.T) {
	ensureTestVFlagSetT(t)

	now := time.Now().UTC()
	rule := &adxmonv1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-retry", Annotations: map[string]string{adxmonv1.SummaryRuleOwnerAnnotation: adxmonv1.SummaryRuleOwnerADXExporter}},
		Spec:       adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: 24 * time.Hour}, Body: "Body"},
		Status:     adxmonv1.SummaryRuleStatus{},
	}
	// Seed with an async op to retry
	start := now.Add(-2 * time.Hour).UTC()
	end := start.Add(time.Hour).Add(-kustoutil.OneTick)
	rule.SetAsyncOperation(adxmonv1.AsyncOperation{OperationId: "retry-op-1", StartTime: start.Format(time.RFC3339Nano), EndTime: end.Format(time.RFC3339Nano)})
	// Prevent new submission in this test
	rule.SetLastExecutionTime(now)

	c := newFakeClientWithRule(t, rule)
	mock := NewMockKustoExecutor(t, "testdb", "https://test")

	// First Mgmt: getOperation -> ShouldRetry=1
	cols := table.Columns{{Name: "LastUpdatedOn", Type: types.DateTime}, {Name: "OperationId", Type: types.String}, {Name: "State", Type: types.String}, {Name: "ShouldRetry", Type: types.Real}, {Name: "Status", Type: types.String}}
	rows := []value.Values{{
		value.DateTime{Value: now, Valid: true},
		value.String{Value: "retry-op-1", Valid: true},
		value.String{Value: "InProgress", Valid: true},
		value.Real{Value: 1, Valid: true},
		value.String{Value: "", Valid: true},
	}}
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, cols, rows))
	// Second Mgmt: resubmission returns new operation id (single-cell)
	op2Cols := table.Columns{{Name: "OperationId", Type: types.String}}
	op2Rows := []value.Values{{value.String{Value: "new-op-2", Valid: true}}}
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, op2Cols, op2Rows))

	r := newBaseReconciler(t, c, mock, now)
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(rule)})
	require.NoError(t, err)

	var updated adxmonv1.SummaryRule
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(rule), &updated))
	ops := updated.GetAsyncOperations()
	require.Len(t, ops, 1)
	require.Equal(t, "new-op-2", ops[0].OperationId)
}

func TestSummaryRule_CriteriaAndDatabaseGating(t *testing.T) {
	ensureTestVFlagSetT(t)
	now := time.Now()
	// Rule for other DB should be skipped
	ruleOtherDB := &adxmonv1.SummaryRule{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-skip-db"}, Spec: adxmonv1.SummaryRuleSpec{Database: "otherdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body"}}
	c := newFakeClientWithRule(t, ruleOtherDB)
	mock := NewMockKustoExecutor(t, "testdb", "https://test")
	r := newBaseReconciler(t, c, mock, now)
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ruleOtherDB)})
	require.NoError(t, err)

	// Rule with non-matching criteria should be skipped
	ruleCriteria := &adxmonv1.SummaryRule{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-skip-criteria"}, Spec: adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body", Criteria: map[string][]string{"region": []string{"westus"}}}}
	// Add object to client
	require.NoError(t, c.Create(context.Background(), ruleCriteria))
	_, err = r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ruleCriteria)})
	require.NoError(t, err)
}

func TestSummaryRule_NoImmediateRetryAfterFailure(t *testing.T) {
	ensureTestVFlagSetT(t)
	now := time.Now()
	rule := &adxmonv1.SummaryRule{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-no-retry", Annotations: map[string]string{adxmonv1.SummaryRuleOwnerAnnotation: adxmonv1.SummaryRuleOwnerADXExporter}}, Spec: adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body"}}
	c := newFakeClientWithRule(t, rule)
	mock := NewMockKustoExecutor(t, "testdb", "https://test")
	// Fail initial submission and backlog retry
	mock.SetNextError(context.DeadlineExceeded)
	mock.SetNextError(context.DeadlineExceeded)
	r := newBaseReconciler(t, c, mock, now)

	// First run: should set condition false and add backlog op
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(rule)})
	require.NoError(t, err)
	var updated1 adxmonv1.SummaryRule
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(rule), &updated1))
	cond := updated1.GetCondition()
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	ops := updated1.GetAsyncOperations()
	require.NotEmpty(t, ops)

	// Second immediate run: with same time and long interval, reconciler should not create new submissions (only backlog attempts, which we also fail once)
	mock.SetNextError(context.DeadlineExceeded)
	_, err = r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(rule)})
	require.NoError(t, err)
}

func TestSummaryRule_BacklogTimestampForwardProgressOnly(t *testing.T) {
	ensureTestVFlagSetT(t)
	now := time.Now().UTC().Truncate(time.Hour)
	// Prepare a rule with last exec at now and a backlog op that would not be forward progress
	rule := &adxmonv1.SummaryRule{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-forward", Annotations: map[string]string{adxmonv1.SummaryRuleOwnerAnnotation: adxmonv1.SummaryRuleOwnerADXExporter}}, Spec: adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body"}}
	rule.SetLastExecutionTime(now)
	prevStart := now.Add(-time.Hour)
	prevEndInclusive := prevStart.Add(time.Hour).Add(-kustoutil.OneTick)
	rule.SetAsyncOperation(adxmonv1.AsyncOperation{OperationId: "", StartTime: prevStart.Format(time.RFC3339Nano), EndTime: prevEndInclusive.Format(time.RFC3339Nano)})

	c := newFakeClientWithRule(t, rule)
	mock := NewMockKustoExecutor(t, "testdb", "https://test")
	// Simulate successful submit of backlog
	opCols := table.Columns{{Name: "OperationId", Type: types.String}}
	opRows := []value.Values{{value.String{Value: "backlog-op-1", Valid: true}}}
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, opCols, opRows))
	r := newBaseReconciler(t, c, mock, now)

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(rule)})
	require.NoError(t, err)

	var updated adxmonv1.SummaryRule
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(rule), &updated))
	// LastExecutionTime should remain at now (no backward movement)
	let := updated.GetLastExecutionTime()
	require.NotNil(t, let)
	require.True(t, let.Equal(now))
}

func TestSummaryRule_MixedAsyncStates(t *testing.T) {
	ensureTestVFlagSetT(t)
	now := time.Now().UTC()
	rule := &adxmonv1.SummaryRule{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-mixed", Annotations: map[string]string{adxmonv1.SummaryRuleOwnerAnnotation: adxmonv1.SummaryRuleOwnerADXExporter}}, Spec: adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body"}}
	// Seed multiple ops
	rule.SetAsyncOperation(adxmonv1.AsyncOperation{OperationId: "completed-op", StartTime: now.Add(-2 * time.Hour).Format(time.RFC3339Nano), EndTime: now.Add(-time.Hour).Format(time.RFC3339Nano)})
	rule.SetAsyncOperation(adxmonv1.AsyncOperation{OperationId: "retry-op", StartTime: now.Add(-time.Hour).Format(time.RFC3339Nano), EndTime: now.Add(-time.Minute).Format(time.RFC3339Nano)})
	c := newFakeClientWithRule(t, rule)
	mock := NewMockKustoExecutor(t, "testdb", "https://test")

	// getOperation for completed-op
	cols := table.Columns{{Name: "LastUpdatedOn", Type: types.DateTime}, {Name: "OperationId", Type: types.String}, {Name: "State", Type: types.String}, {Name: "ShouldRetry", Type: types.Real}, {Name: "Status", Type: types.String}}
	rowsCompleted := []value.Values{{value.DateTime{Value: now, Valid: true}, value.String{Value: "completed-op", Valid: true}, value.String{Value: "Completed", Valid: true}, value.Real{Value: 0, Valid: true}, value.String{Value: "", Valid: true}}}
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, cols, rowsCompleted))
	// getOperation for retry-op (ShouldRetry=1)
	rowsRetry := []value.Values{{value.DateTime{Value: now, Valid: true}, value.String{Value: "retry-op", Valid: true}, value.String{Value: "InProgress", Valid: true}, value.Real{Value: 1, Valid: true}, value.String{Value: "", Valid: true}}}
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, cols, rowsRetry))
	// resubmission new id
	opCols := table.Columns{{Name: "OperationId", Type: types.String}}
	opRows := []value.Values{{value.String{Value: "retry-op-new", Valid: true}}}
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, opCols, opRows))

	r := newBaseReconciler(t, c, mock, now)
	// Prevent fresh submission by persisting LastExecutionTime in status
	var fetched adxmonv1.SummaryRule
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(rule), &fetched))
	fetched.SetLastExecutionTime(now)
	require.NoError(t, c.Status().Update(context.Background(), &fetched))
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(rule)})
	require.NoError(t, err)

	var updated adxmonv1.SummaryRule
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(rule), &updated))
	ops := updated.GetAsyncOperations()
	require.Len(t, ops, 1)
	require.Equal(t, "retry-op-new", ops[0].OperationId)
}

func TestSummaryRule_GetOperationFailureRetainsRecentOps(t *testing.T) {
	ensureTestVFlagSetT(t)
	now := time.Now().UTC()
	rule := &adxmonv1.SummaryRule{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-retain", Annotations: map[string]string{adxmonv1.SummaryRuleOwnerAnnotation: adxmonv1.SummaryRuleOwnerADXExporter}}, Spec: adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body"}}
	// Seed with one recent op id
	rule.SetAsyncOperation(adxmonv1.AsyncOperation{OperationId: "recent-op", StartTime: now.Add(-time.Hour).Format(time.RFC3339Nano), EndTime: now.Add(-time.Minute).Format(time.RFC3339Nano)})
	// Prevent new submission
	rule.SetLastExecutionTime(now)
	c := newFakeClientWithRule(t, rule)
	mock := NewMockKustoExecutor(t, "testdb", "https://test")
	// Simulate getOperation error
	mock.SetNextError(context.Canceled)
	r := newBaseReconciler(t, c, mock, now)
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(rule)})
	require.NoError(t, err)
	var updated adxmonv1.SummaryRule
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(rule), &updated))
	ops := updated.GetAsyncOperations()
	require.Len(t, ops, 1)
	require.Equal(t, "recent-op", ops[0].OperationId)
}

func TestSummaryRule_KustoErrorParsingOnFailure(t *testing.T) {
	ensureTestVFlagSetT(t)
	now := time.Now().UTC()
	rule := &adxmonv1.SummaryRule{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-err-msg", Annotations: map[string]string{adxmonv1.SummaryRuleOwnerAnnotation: adxmonv1.SummaryRuleOwnerADXExporter}}, Spec: adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body"}}
	c := newFakeClientWithRule(t, rule)
	mock := NewMockKustoExecutor(t, "testdb", "https://test")
	// Create a Kusto HttpError with an @message payload and wrap it
	body := `{"error":{"@message": "function is invalid"}}`
	kerr := kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")
	wrapped := fmt.Errorf("exec failed: %w", kerr)
	mock.errors = append(mock.errors, wrapped)
	// Also fail backlog resubmission during same reconcile to avoid unintended empty iterator path
	mock.errors = append(mock.errors, wrapped)
	r := newBaseReconciler(t, c, mock, now)
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(rule)})
	require.NoError(t, err)
	var updated adxmonv1.SummaryRule
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(rule), &updated))
	cond := updated.GetCondition()
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, "function is invalid", cond.Message)
}

func TestSummaryRule_ExporterSkipsWhenOwnerIngestorOrMissing(t *testing.T) {
	ensureTestVFlagSetT(t)
	now := time.Now()

	// Case 1: owner=ingestor -> exporter should skip
	ruleIngestor := &adxmonv1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-skip-ingestor", Annotations: map[string]string{adxmonv1.SummaryRuleOwnerAnnotation: adxmonv1.SummaryRuleOwnerIngestor}},
		Spec:       adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body"},
	}
	// Case 2: no owner annotation -> exporter should skip
	ruleMissing := &adxmonv1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-skip-missing"},
		Spec:       adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body"},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, adxmonv1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&adxmonv1.SummaryRule{}).WithObjects(ruleIngestor, ruleMissing).Build()

	mock := NewMockKustoExecutor(t, "testdb", "https://test")
	r := newBaseReconciler(t, c, mock, now)

	// Run reconcile on both
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ruleIngestor)})
	require.NoError(t, err)
	_, err = r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ruleMissing)})
	require.NoError(t, err)

	var updated1, updated2 adxmonv1.SummaryRule
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(ruleIngestor), &updated1))
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(ruleMissing), &updated2))
	require.Len(t, updated1.GetAsyncOperations(), 0)
	require.Nil(t, updated1.GetCondition())
	require.Len(t, updated2.GetAsyncOperations(), 0)
	require.Nil(t, updated2.GetCondition())
}

func TestSummaryRule_ExporterAdoptsWhenDesiredAndSafe(t *testing.T) {
	ensureTestVFlagSetT(t)
	now := time.Now().UTC()
	rule := &adxmonv1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "default",
			Name:        "sr-adopt-safe",
			Annotations: map[string]string{adxmonv1.SummaryRuleDesiredOwnerAnnotation: adxmonv1.SummaryRuleOwnerADXExporter},
		},
		Spec: adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body"},
	}
	// Make ShouldSubmitRule return false by setting last exec to now
	rule.SetLastExecutionTime(now)

	c := newFakeClientWithRule(t, rule)
	mock := NewMockKustoExecutor(t, "testdb", "https://test")
	r := newBaseReconciler(t, c, mock, now)

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(rule)})
	require.NoError(t, err)
	require.Equal(t, 5*time.Second, res.RequeueAfter)

	// Verify owner is set and desired-owner cleared
	var updated adxmonv1.SummaryRule
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(rule), &updated))
	require.Equal(t, adxmonv1.SummaryRuleOwnerADXExporter, updated.Annotations[adxmonv1.SummaryRuleOwnerAnnotation])
	_, exists := updated.Annotations[adxmonv1.SummaryRuleDesiredOwnerAnnotation]
	require.False(t, exists)
}

func TestSummaryRule_ExporterDoesNotAdoptWhenInsideWindow(t *testing.T) {
	ensureTestVFlagSetT(t)
	now := time.Now().UTC()
	rule := &adxmonv1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "default",
			Name:        "sr-adopt-unsafe",
			Annotations: map[string]string{adxmonv1.SummaryRuleDesiredOwnerAnnotation: adxmonv1.SummaryRuleOwnerADXExporter},
		},
		Spec: adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body"},
	}
	// Make ShouldSubmitRule return true: set last exec to now-interval
	rule.SetLastExecutionTime(now.Add(-time.Hour))

	c := newFakeClientWithRule(t, rule)
	mock := NewMockKustoExecutor(t, "testdb", "https://test")
	r := newBaseReconciler(t, c, mock, now)

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(rule)})
	require.NoError(t, err)
	// Should skip and not requeue immediately for adoption
	require.Equal(t, time.Duration(0), res.RequeueAfter)

	var updated adxmonv1.SummaryRule
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(rule), &updated))
	// Owner should remain unset, desired still present
	_, hasOwner := updated.Annotations[adxmonv1.SummaryRuleOwnerAnnotation]
	require.False(t, hasOwner)
	require.Equal(t, adxmonv1.SummaryRuleOwnerADXExporter, updated.Annotations[adxmonv1.SummaryRuleDesiredOwnerAnnotation])
}

func TestSummaryRule_OneTickBoundary_NoDoubleProcessing(t *testing.T) {
	ensureTestVFlagSetT(t)
	// Window boundaries should subtract OneTick for inclusive end, preventing overlap with next window
	start := time.Date(2025, 8, 13, 10, 0, 0, 0, time.UTC)
	rule := &adxmonv1.SummaryRule{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-onetick", Annotations: map[string]string{adxmonv1.SummaryRuleOwnerAnnotation: adxmonv1.SummaryRuleOwnerADXExporter}}, Spec: adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body"}}
	c := newFakeClientWithRule(t, rule)
	mock := NewMockKustoExecutor(t, "testdb", "https://test")
	// First submission returns op id and then getOperation for that id
	opCols := table.Columns{{Name: "OperationId", Type: types.String}}
	op1 := []value.Values{{value.String{Value: "op-1", Valid: true}}}
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, opCols, op1))
	statusCols := table.Columns{{Name: "LastUpdatedOn", Type: types.DateTime}, {Name: "OperationId", Type: types.String}, {Name: "State", Type: types.String}, {Name: "ShouldRetry", Type: types.Real}, {Name: "Status", Type: types.String}}
	inprog := []value.Values{{value.DateTime{Value: start, Valid: true}, value.String{Value: "op-1", Valid: true}, value.String{Value: "InProgress", Valid: true}, value.Real{Value: 0, Valid: true}, value.String{Value: "", Valid: true}}}
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, statusCols, inprog))
	r := newBaseReconciler(t, c, mock, start)
	// Run reconcile to submit first window [10:00, 11:00-1tick]
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(rule)})
	require.NoError(t, err)
	// Advance clock by exactly one interval and submit next window; provide results for submission and both getOperation calls
	op2 := []value.Values{{value.String{Value: "op-2", Valid: true}}}
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, opCols, op2))
	// getOperation for op-1 again
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, statusCols, inprog))
	// getOperation for op-2
	inprog2 := []value.Values{{value.DateTime{Value: start.Add(time.Hour), Valid: true}, value.String{Value: "op-2", Valid: true}, value.String{Value: "InProgress", Valid: true}, value.Real{Value: 0, Valid: true}, value.String{Value: "", Valid: true}}}
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, statusCols, inprog2))
	r.Clock.(*klock.FakeClock).SetTime(start.Add(time.Hour))
	_, err = r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(rule)})
	require.NoError(t, err)
	// Inspect the two submission Mgmt queries and ensure they are different (reflecting different windows)
	var submissions []string
	for _, q := range mock.GetQueries() {
		if strings.HasPrefix(q, ".set-or-append async ") {
			submissions = append(submissions, q)
		}
	}
	require.Len(t, submissions, 2)
	require.NotEqual(t, submissions[0], submissions[1])
}

func TestSummaryRule_getOperation_Parsing(t *testing.T) {
	ensureTestVFlagSetT(t)
	now := time.Now().UTC()
	c := newFakeClientWithRule(t, &adxmonv1.SummaryRule{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "dummy"}, Spec: adxmonv1.SummaryRuleSpec{Database: "testdb"}})
	mock := NewMockKustoExecutor(t, "testdb", "https://test")
	r := newBaseReconciler(t, c, mock, now)

	// Configure a valid status row
	cols := table.Columns{{Name: "LastUpdatedOn", Type: types.DateTime}, {Name: "OperationId", Type: types.String}, {Name: "State", Type: types.String}, {Name: "ShouldRetry", Type: types.Real}, {Name: "Status", Type: types.String}}
	rows := []value.Values{{value.DateTime{Value: now, Valid: true}, value.String{Value: "op-x", Valid: true}, value.String{Value: "Completed", Valid: true}, value.Real{Value: 0, Valid: true}, value.String{Value: "", Valid: true}}}
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, cols, rows))
	st, err := r.getOperation(context.Background(), "testdb", "op-x")
	require.NoError(t, err)
	require.NotNil(t, st)
	require.Equal(t, "op-x", st.OperationId)
	require.Equal(t, "Completed", st.State)
	require.Equal(t, float64(0), st.ShouldRetry)

	// Now configure empty result and expect nil
	mock.results = append(mock.results, createRowIteratorFromMockRows(t, cols, nil))
	st, err = r.getOperation(context.Background(), "testdb", "not-found")
	require.NoError(t, err)
	require.Nil(t, st)
}
