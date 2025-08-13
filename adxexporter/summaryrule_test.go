package adxexporter

import (
    "context"
    "flag"
    "testing"
    "time"

    adxmonv1 "github.com/Azure/adx-mon/api/v1"
    "github.com/Azure/adx-mon/pkg/kustoutil"
    "github.com/Azure/azure-kusto-go/kusto"
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

// test helper: ensure test.v set for Azure Kusto SDK mocks
func ensureTestVFlagSetT(t *testing.T) {
    t.Helper()
    if flag.Lookup("test.v") == nil {
        flag.String("test.v", "", "")
        err := flag.CommandLine.Set("test.v", "true")
        require.NoError(t, err)
    }
}

// test helper: create a RowIterator from custom columns/values
func createRowIteratorFromMockRows(t *testing.T, cols table.Columns, rows []value.Values) *kusto.RowIterator {
    t.Helper()
    iter := &kusto.RowIterator{}
    mockRows, err := kusto.NewMockRows(cols)
    require.NoError(t, err)
    for _, r := range rows {
        require.NoError(t, mockRows.Row(r))
    }
    ensureTestVFlagSetT(t)
    require.NoError(t, iter.Mock(mockRows))
    return iter
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
        QueryExecutors: map[string]*QueryExecutor{},
        KustoExecutors: map[string]KustoExecutor{"testdb": mock},
        Clock:          klock.NewFakeClock(clockNow),
    }
}

func TestSummaryRule_SubmissionSuccess(t *testing.T) {
    ensureTestVFlagSetT(t)

    rule := &adxmonv1.SummaryRule{
        ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr"},
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
        ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-fail"},
        Spec: adxmonv1.SummaryRuleSpec{Database: "testdb", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "Body"},
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
        ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-failed-op"},
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
        ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr-retry"},
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
