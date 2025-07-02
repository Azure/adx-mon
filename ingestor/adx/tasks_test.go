package adx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/kustoutil"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/azure-kusto-go/kusto"
	kustoerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/Azure/azure-kusto-go/kusto/data/value"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// mockCRDHandler implements storage.CRDHandler interface for testing
type mockCRDHandler struct {
	listResponse   client.ObjectList
	listError      error
	updateStatusFn func(context.Context, client.Object, error) error
	updatedObjects []client.Object
}

func (m *mockCRDHandler) List(ctx context.Context, list client.ObjectList, filters ...storage.ListFilterFunc) error {
	if m.listError != nil {
		return m.listError
	}

	// Copy the response items to the provided list
	if m.listResponse != nil {
		switch list := list.(type) {
		case *v1.SummaryRuleList:
			if srList, ok := m.listResponse.(*v1.SummaryRuleList); ok {
				list.Items = srList.Items
			}
		}
	}

	return nil
}

func (m *mockCRDHandler) UpdateStatus(ctx context.Context, obj client.Object, errStatus error) error {
	if m.updateStatusFn != nil {
		return m.updateStatusFn(ctx, obj, errStatus)
	}

	// Implement the actual UpdateStatus logic to set conditions properly
	statusObj, ok := obj.(adxmonv1.ConditionedObject)
	if !ok {
		return errors.New("object does not implement ConditionedObject")
	}

	var (
		status  = metav1.ConditionTrue
		message = ""
	)
	if errStatus != nil {
		status = metav1.ConditionFalse
		message = errStatus.Error()
	}

	condition := metav1.Condition{
		Status:  status,
		Message: message,
	}

	statusObj.SetCondition(condition)

	// Track updated objects for assertions in tests
	m.updatedObjects = append(m.updatedObjects, obj.DeepCopyObject().(client.Object))
	return nil
}

func (m *mockCRDHandler) UpdateStatusWithKustoErrorParsing(ctx context.Context, obj client.Object, errStatus error) error {
	if m.updateStatusFn != nil {
		return m.updateStatusFn(ctx, obj, errStatus)
	}

	// Implement the actual UpdateStatusWithKustoErrorParsing logic to set conditions properly
	statusObj, ok := obj.(adxmonv1.ConditionedObject)
	if !ok {
		return errors.New("object does not implement ConditionedObject")
	}

	var (
		status  = metav1.ConditionTrue
		message = ""
	)
	if errStatus != nil {
		status = metav1.ConditionFalse
		message = kustoutil.ParseError(errStatus)
	}

	condition := metav1.Condition{
		Status:  status,
		Message: message,
	}

	statusObj.SetCondition(condition)

	// Track updated objects for assertions in tests
	m.updatedObjects = append(m.updatedObjects, obj.DeepCopyObject().(client.Object))
	return nil
}

type TestStatementExecutor struct {
	database    string
	endpoint    string
	stmts       []string
	nextMgmtErr error
	operationID string
}

func (t *TestStatementExecutor) Database() string {
	return t.database
}

func (t *TestStatementExecutor) Endpoint() string {
	return t.endpoint
}

func (t *TestStatementExecutor) Mgmt(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
	t.stmts = append(t.stmts, query.String())
	if t.nextMgmtErr != nil {
		ret := t.nextMgmtErr
		t.nextMgmtErr = nil
		return nil, ret
	}

	// For ClusterLabels tests, we need to return a mock result that simulates an operation ID
	// Since we're mainly testing the query transformation, we can return a mock iterator
	// that provides an operation ID when needed
	if t.operationID != "" {
		// This is a simplified mock - in real usage, the RowIterator would contain
		// the operation ID from Kusto. For our tests, we'll work around this limitation.
		return &kusto.RowIterator{}, nil
	}

	return nil, nil
}

type TestFunctionStore struct {
	funcs         []*v1.Function
	updates       []*v1.Function
	statusUpdates []*v1.Function
	nextUpdateErr error
}

func (t *TestFunctionStore) List(ctx context.Context) ([]*v1.Function, error) {
	ret := make([]*v1.Function, len(t.funcs))
	for i, f := range t.funcs {
		ret[i] = f.DeepCopy()
	}
	return ret, nil
}

func (t *TestFunctionStore) Update(ctx context.Context, fn *v1.Function) error {
	t.updates = append(t.updates, fn.DeepCopy())
	if t.nextUpdateErr != nil {
		ret := t.nextUpdateErr
		t.nextUpdateErr = nil
		return ret
	}
	for i, f := range t.funcs {
		if f.Name == fn.Name && f.Namespace == fn.Namespace {
			t.funcs[i] = fn.DeepCopy()
			break
		}
	}
	return nil
}

func (t *TestFunctionStore) UpdateStatus(ctx context.Context, fn *v1.Function) error {
	t.statusUpdates = append(t.statusUpdates, fn.DeepCopy())
	if t.nextUpdateErr != nil {
		ret := t.nextUpdateErr
		t.nextUpdateErr = nil
		return ret
	}
	for i, f := range t.funcs {
		if f.Name == fn.Name && f.Namespace == fn.Namespace {
			t.funcs[i].Status = *fn.Status.DeepCopy()
			break
		}
	}
	return nil
}

func TestUpdateKQLFunctionStatus(t *testing.T) {
	t.Run("update status without error", func(t *testing.T) {
		fn := &v1.Function{
			Status: v1.FunctionStatus{
				Status: v1.Failed,
			},
		}
		task := &SyncFunctionsTask{
			store: &TestFunctionStore{},
		}
		require.NoError(t, task.updateKQLFunctionStatus(context.Background(), fn, v1.Success, nil))
		require.Equal(t, v1.Success, fn.Status.Status)
		require.Empty(t, fn.Status.Error)
	})

	t.Run("handles error from function store", func(t *testing.T) {
		fn := &v1.Function{
			Status: v1.FunctionStatus{
				Status: v1.Failed,
			},
		}
		task := &SyncFunctionsTask{
			store: &TestFunctionStore{
				nextUpdateErr: io.EOF,
			},
		}
		require.Error(t, task.updateKQLFunctionStatus(context.Background(), fn, v1.Success, nil))
	})

	t.Run("update status with non-kusto-http error", func(t *testing.T) {
		fn := &v1.Function{
			Status: v1.FunctionStatus{
				Status: v1.Failed,
			},
		}
		task := &SyncFunctionsTask{
			store: &TestFunctionStore{},
		}
		require.NoError(t, task.updateKQLFunctionStatus(context.Background(), fn, v1.Failed, io.EOF))
		require.Equal(t, v1.Failed, fn.Status.Status)
		require.Equal(t, io.EOF.Error(), fn.Status.Error)

		// Requires truncation
		msg := strings.Repeat("a", 300)
		require.NoError(t, task.updateKQLFunctionStatus(context.Background(), fn, v1.Failed, errors.New(msg)))
		require.Equal(t, v1.Failed, fn.Status.Status)
		require.Equal(t, strings.Repeat("a", 256), fn.Status.Error)
	})

	t.Run("update status with kusto-http error", func(t *testing.T) {
		fn := &v1.Function{
			Status: v1.FunctionStatus{
				Status: v1.Failed,
			},
		}
		body := `{"error":{"@message": "this function is invalid"}}`
		funcErr := kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		task := &SyncFunctionsTask{
			store: &TestFunctionStore{},
		}
		require.NoError(t, task.updateKQLFunctionStatus(context.Background(), fn, v1.Failed, funcErr))
		require.Equal(t, v1.Failed, fn.Status.Status)
		require.Equal(t, "this function is invalid", fn.Status.Error)

		// Requires truncation
		msg := strings.Repeat("a", 300)
		body = fmt.Sprintf(`{"error":{"@message": "%s"}}`, msg)
		funcErr = kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")
		require.NoError(t, task.updateKQLFunctionStatus(context.Background(), fn, v1.Failed, funcErr))
		require.Equal(t, v1.Failed, fn.Status.Status)
		require.Equal(t, strings.Repeat("a", 256), fn.Status.Error)
	})
}

func TestFunctions(t *testing.T) {
	testutils.IntegrationTest(t)

	scheme := clientgoscheme.Scheme
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, adxmonv1.AddToScheme(scheme))

	ctx := context.Background()
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	require.NoError(t, testutils.InstallCrds(ctx, k3sContainer))

	kustoContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithCluster(ctx, k3sContainer))
	testcontainers.CleanupContainer(t, kustoContainer)
	require.NoError(t, err)

	restConfig, ctrlCli, err := testutils.GetKubeConfig(ctx, k3sContainer)
	require.NoError(t, err)
	require.NoError(t, kustoContainer.PortForward(ctx, restConfig))

	cb := kusto.NewConnectionStringBuilder(kustoContainer.ConnectionUrl())
	kustoClient, err := kusto.New(cb)
	require.NoError(t, err)
	defer kustoClient.Close()

	executor := &KustoStatementExecutor{
		database: "NetDefaultDB",
		client:   kustoClient,
	}

	functionStore := storage.NewFunctions(ctrlCli, nil)
	task := NewSyncFunctionsTask(functionStore, executor)

	resourceName := "testtest"
	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	t.Run("Creates functions", func(t *testing.T) {
		fn := &adxmonv1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typeNamespacedName.Name,
				Namespace: typeNamespacedName.Namespace,
			},
			Spec: adxmonv1.FunctionSpec{
				Body:     ".create-or-alter function testtest() { print 'Hello World' }",
				Database: executor.Database(),
			},
		}
		require.NoError(t, ctrlCli.Create(ctx, fn))

		require.NoError(t, task.Run(ctx))

		require.Eventually(t, func() bool {
			return testutils.FunctionExists(ctx, t, executor.Database(), resourceName, kustoContainer.ConnectionUrl())
		}, 10*time.Minute, time.Second)
	})

	t.Run("Creates functions for all databases", func(t *testing.T) {
		resourceName := "testalldb"
		fn := &adxmonv1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: typeNamespacedName.Namespace,
			},
			Spec: adxmonv1.FunctionSpec{
				Body:     fmt.Sprintf(".create-or-alter function %s() { print 'Hello World' }", resourceName),
				Database: v1.AllDatabases,
			},
		}
		require.NoError(t, ctrlCli.Create(ctx, fn))

		require.NoError(t, task.Run(ctx))

		require.Eventually(t, func() bool {
			return testutils.FunctionExists(ctx, t, executor.Database(), resourceName, kustoContainer.ConnectionUrl())
		}, 10*time.Minute, time.Second)
	})

	t.Run("Updates functions", func(t *testing.T) {
		fn := &adxmonv1.Function{}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, fn))

		fn.Spec.Body = ".create-or-alter function testtest() { print 'Hello World 2' }"
		require.NoError(t, ctrlCli.Update(ctx, fn))

		require.NoError(t, task.Run(ctx))

		require.Eventually(t, func() bool {
			fn := testutils.GetFunction(ctx, t, executor.Database(), resourceName, kustoContainer.ConnectionUrl())
			return strings.Contains(fn.Body, "Hello World 2")
		}, 10*time.Minute, time.Second)
	})

	t.Run("Deletes functions", func(t *testing.T) {
		fn := &adxmonv1.Function{}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, fn))
		require.NoError(t, ctrlCli.Delete(ctx, fn))

		require.NoError(t, task.Run(ctx))

		require.Eventually(t, func() bool {
			return !testutils.FunctionExists(ctx, t, executor.Database(), resourceName, kustoContainer.ConnectionUrl())
		}, 10*time.Minute, time.Second)
	})

	t.Run("Creates more than one function", func(t *testing.T) {
		fn := &adxmonv1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "a-and-b",
				Namespace: typeNamespacedName.Namespace,
			},
			Spec: adxmonv1.FunctionSpec{
				Body:     severalFunctions,
				Database: executor.Database(),
			},
		}
		require.NoError(t, ctrlCli.Create(ctx, fn))
		require.NoError(t, task.Run(ctx))

		require.Eventually(t, func() bool {
			return testutils.FunctionExists(ctx, t, executor.Database(), "a", kustoContainer.ConnectionUrl()) &&
				testutils.FunctionExists(ctx, t, executor.Database(), "b", kustoContainer.ConnectionUrl())
		}, 10*time.Minute, time.Second)
	})

	t.Run("Communicates invalid functions", func(t *testing.T) {
		// To support more than one function in a single CRD, we execute the CRD body
		// as a database script. By default, database scripts that contain error do
		// not telegraph individual errors, only if the entire script fails. We set
		// sufficient options to enable any failures within the script body to bubble
		// back to the caller, so this test ensures that an invalid function body
		// is communicated back to the caller.
		resourceName := "invalid-function"
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		fn := &adxmonv1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typeNamespacedName.Name,
				Namespace: typeNamespacedName.Namespace,
			},
			Spec: adxmonv1.FunctionSpec{
				Body:     ".create-or-alter function() { MissingTable | count }",
				Database: executor.Database(),
			},
		}
		require.NoError(t, ctrlCli.Create(ctx, fn))
		require.NoError(t, task.Run(ctx))

		require.Eventually(t, func() bool {
			fnr := &adxmonv1.Function{}
			require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, fnr))
			return fnr.Status.Status == v1.PermanentFailure
		}, 10*time.Minute, time.Second)
	})
}

func TestManagementCommands(t *testing.T) {
	testutils.IntegrationTest(t)

	scheme := clientgoscheme.Scheme
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, adxmonv1.AddToScheme(scheme))

	ctx := context.Background()
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	require.NoError(t, testutils.InstallCrds(ctx, k3sContainer))

	kustoContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithCluster(ctx, k3sContainer))
	testcontainers.CleanupContainer(t, kustoContainer)
	require.NoError(t, err)

	restConfig, ctrlCli, err := testutils.GetKubeConfig(ctx, k3sContainer)
	require.NoError(t, err)
	require.NoError(t, kustoContainer.PortForward(ctx, restConfig))

	cb := kusto.NewConnectionStringBuilder(kustoContainer.ConnectionUrl())
	kustoClient, err := kusto.New(cb)
	require.NoError(t, err)
	defer kustoClient.Close()

	executor := &KustoStatementExecutor{
		database: "NetDefaultDB",
		client:   kustoClient,
	}

	store := storage.NewCRDHandler(ctrlCli, nil)
	task := NewManagementCommandsTask(store, executor)

	t.Run("Creates database management commands", func(t *testing.T) {
		resourceName := "testtest"
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		fn := &adxmonv1.ManagementCommand{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typeNamespacedName.Name,
				Namespace: typeNamespacedName.Namespace,
			},
			Spec: adxmonv1.ManagementCommandSpec{
				Body:     ".clear database cache query_results",
				Database: executor.Database(),
			},
		}
		require.NoError(t, ctrlCli.Create(ctx, fn))
		require.NoError(t, task.Run(ctx))

		require.Eventually(t, func() bool {
			cmd := &adxmonv1.ManagementCommand{}
			require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, cmd))

			// wait for the command to be marked as owner completed successfully
			for _, condition := range cmd.Status.Conditions {
				if condition.Type == adxmonv1.ManagementCommandConditionOwner {
					return condition.Status == metav1.ConditionTrue
				}
			}

			return false
		}, 10*time.Minute, time.Second)
	})

	t.Run("Creates cluster management commands", func(t *testing.T) {
		resourceName := "testtesttest"
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		fn := &adxmonv1.ManagementCommand{
			ObjectMeta: metav1.ObjectMeta{
				Name:      typeNamespacedName.Name,
				Namespace: typeNamespacedName.Namespace,
			},
			Spec: adxmonv1.ManagementCommandSpec{
				Body: `.alter cluster policy request_classification '{"IsEnabled":true}' <|
    iff(request_properties.current_application == "Kusto.Explorer" and request_properties.request_type == "Query",
        "Ad-hoc queries",
        "default")`,
			},
		}
		require.NoError(t, ctrlCli.Create(ctx, fn))
		require.NoError(t, task.Run(ctx))

		require.Eventually(t, func() bool {
			cmd := &adxmonv1.ManagementCommand{}
			require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, cmd))

			// wait for the command to be marked as owner completed successfully
			for _, condition := range cmd.Status.Conditions {
				if condition.Type == adxmonv1.ManagementCommandConditionOwner {
					return condition.Status == metav1.ConditionTrue
				}
			}

			return false
		}, 10*time.Minute, time.Second)
	})
}

type KustoStatementExecutor struct {
	database string
	client   *kusto.Client
}

func (k *KustoStatementExecutor) Database() string {
	return k.database
}

func (k *KustoStatementExecutor) Endpoint() string {
	return k.client.Endpoint()
}

func (k *KustoStatementExecutor) Mgmt(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
	return k.client.Mgmt(ctx, k.database, query, options...)
}

func TestSummaryRuleSubmissionFailure(t *testing.T) {
	// Create a mock statement executor
	mockExecutor := &TestStatementExecutor{
		database: "testdb",
		endpoint: "http://test-endpoint",
	}

	// Create a summary rule
	ruleName := "test-rule"
	rule := &v1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: ruleName,
		},
		Spec: v1.SummaryRuleSpec{
			Database: "testdb",
			Table:    "TestTable",
			Interval: metav1.Duration{Duration: time.Hour},
			Body:     "TestBody",
		},
	}

	// Create a list to be returned by the mock handler
	ruleList := &v1.SummaryRuleList{
		Items: []v1.SummaryRule{*rule},
	}

	// Create a mock handler that will return our rule and track updates
	mockHandler := &mockCRDHandler{
		listResponse:   ruleList,
		updatedObjects: []client.Object{},
	}

	// Create the task with our mocks
	task := &SummaryRuleTask{
		store:    mockHandler,
		kustoCli: mockExecutor,
	}

	// Set the GetOperations function to return an empty list
	task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
		return []AsyncOperationStatus{}, nil
	}

	// Mock the SubmitRule function to return an error
	submissionError := errors.New("invalid KQL query")
	task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
		return "", submissionError
	}

	// Run the task
	err := task.Run(context.Background())
	require.NoError(t, err)

	// Check that the rule was updated with error status only once
	require.Len(t, mockHandler.updatedObjects, 1, "Rule should have been updated exactly once with error status")

	// Check that the rule contains the error
	updatedRule, ok := mockHandler.updatedObjects[0].(*v1.SummaryRule)
	require.True(t, ok, "Updated object should be a SummaryRule")
	require.Equal(t, ruleName, updatedRule.Name, "Rule name should match")

	// Verify the condition shows failure
	condition := updatedRule.GetCondition()
	require.NotNil(t, condition, "Rule should have a condition")
	require.Equal(t, metav1.ConditionFalse, condition.Status, "Condition status should be False")
	require.Equal(t, "Failed", condition.Reason, "Condition reason should be Failed")
	require.Contains(t, condition.Message, "invalid KQL query", "Condition message should contain the error")

	// Should have no async operations since submission failed
	asyncOps := updatedRule.GetAsyncOperations()
	require.Len(t, asyncOps, 1, "Should have an async operation when submission fails")
}

func TestSummaryRuleSubmissionSuccess(t *testing.T) {
	// Create a mock statement executor
	mockExecutor := &TestStatementExecutor{
		database: "testdb",
		endpoint: "http://test-endpoint",
	}

	// Create a summary rule
	ruleName := "test-rule"
	rule := &v1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: ruleName,
		},
		Spec: v1.SummaryRuleSpec{
			Database: "testdb",
			Table:    "TestTable",
			Interval: metav1.Duration{Duration: time.Hour},
			Body:     "TestBody",
		},
	}

	// Create a list to be returned by the mock handler
	ruleList := &v1.SummaryRuleList{
		Items: []v1.SummaryRule{*rule},
	}

	// Create a mock handler that will return our rule and track updates
	mockHandler := &mockCRDHandler{
		listResponse:   ruleList,
		updatedObjects: []client.Object{},
	}

	// Create the task with our mocks
	task := &SummaryRuleTask{
		store:    mockHandler,
		kustoCli: mockExecutor,
	}

	// Set the GetOperations function to return an empty list
	task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
		return []AsyncOperationStatus{}, nil
	}

	// Mock the SubmitRule function to succeed
	task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
		return "operation-id-123", nil
	}

	// Run the task
	err := task.Run(context.Background())
	require.NoError(t, err)

	// Check that the rule was updated once with success status
	require.Len(t, mockHandler.updatedObjects, 1, "Rule should have been updated exactly once")

	// Check that the rule shows success
	updatedRule, ok := mockHandler.updatedObjects[0].(*v1.SummaryRule)
	require.True(t, ok, "Updated object should be a SummaryRule")
	require.Equal(t, ruleName, updatedRule.Name, "Rule name should match")

	// Verify the condition shows success
	condition := updatedRule.GetCondition()
	require.NotNil(t, condition, "Rule should have a condition")
	require.Equal(t, metav1.ConditionTrue, condition.Status, "Condition status should be True")
	require.Empty(t, condition.Message, "Condition message should be empty for success")

	// Should have one async operation since submission succeeded
	asyncOps := updatedRule.GetAsyncOperations()
	require.Len(t, asyncOps, 1, "Should have one async operation when submission succeeds")
	require.Equal(t, "operation-id-123", asyncOps[0].OperationId, "Should have the correct operation ID")
}

func TestAsyncOperationRemoval(t *testing.T) {
	// Create a mock statement executor that returns empty operations list
	mockExecutor := &TestStatementExecutor{
		database: "testdb",
		endpoint: "http://test-endpoint",
	}

	// Create a summary rule with an async operation
	ruleName := "test-rule"
	operationId := "op-1"
	rule := &v1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: ruleName,
		},
		Spec: v1.SummaryRuleSpec{
			Database: "testdb",
			Table:    "TestTable",
			Interval: metav1.Duration{Duration: time.Hour},
			Body:     "TestBody",
		},
	}

	// Add an async operation to the rule
	rule.SetAsyncOperation(v1.AsyncOperation{
		OperationId: operationId,
		StartTime:   "2025-05-22T19:20:00Z",
		EndTime:     "2025-05-22T19:30:00Z",
	})

	// Create a list to be returned by the mock handler
	ruleList := &v1.SummaryRuleList{
		Items: []v1.SummaryRule{*rule},
	}

	// Create a mock handler that will return our rule and track updates
	mockHandler := &mockCRDHandler{
		listResponse:   ruleList,
		updatedObjects: []client.Object{},
	}

	// Create the task with our mocks
	task := &SummaryRuleTask{
		store:    mockHandler,
		kustoCli: mockExecutor,
	}

	// Set the GetOperations function to return an empty list
	task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
		return []AsyncOperationStatus{}, nil
	}

	// Mock the SubmitRule function to avoid actual Kusto operations
	task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
		return "new-operation-id", nil
	}

	// Run the task
	err := task.Run(context.Background())
	require.NoError(t, err)

	// Check that the rule was updated
	require.Len(t, mockHandler.updatedObjects, 1, "Rule should have been updated once")

	// Check that the old async operation was removed but a new one was created
	updatedRule, ok := mockHandler.updatedObjects[0].(*v1.SummaryRule)
	require.True(t, ok, "Updated object should be a SummaryRule")
	require.Equal(t, ruleName, updatedRule.Name, "Rule name should match")

	// Should have exactly 1 operation (the new one), not 0
	asyncOps := updatedRule.GetAsyncOperations()
	require.Len(t, asyncOps, 1, "Should have one async operation (the new one)")
	require.Equal(t, "new-operation-id", asyncOps[0].OperationId, "Should be the new operation")
	require.NotEqual(t, operationId, asyncOps[0].OperationId, "Old operation should be gone")
}

func TestSummaryRuleGetOperationsFailure(t *testing.T) {
	// Create a mock statement executor
	mockExecutor := &TestStatementExecutor{
		database: "testdb",
		endpoint: "http://test-endpoint",
	}

	// Create a summary rule
	ruleName := "test-rule"
	rule := &v1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: ruleName,
		},
		Spec: v1.SummaryRuleSpec{
			Database: "testdb",
			Table:    "TestTable",
			Interval: metav1.Duration{Duration: time.Hour},
			Body:     "TestBody",
		},
	}

	// Create a list to be returned by the mock handler
	ruleList := &v1.SummaryRuleList{
		Items: []v1.SummaryRule{*rule},
	}

	// Create a mock handler that will return our rule and track updates
	mockHandler := &mockCRDHandler{
		listResponse:   ruleList,
		updatedObjects: []client.Object{},
	}

	// Create the task with our mocks
	task := &SummaryRuleTask{
		store:    mockHandler,
		kustoCli: mockExecutor,
	}

	// Set the GetOperations function to return an error (simulating Kusto unavailable)
	getOperationsError := errors.New("failed to connect to Kusto cluster")
	task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
		return nil, getOperationsError
	}

	// Mock the SubmitRule function to succeed
	task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
		return "operation-id-123", nil
	}

	// Run the task - this should now succeed even when GetOperations fails
	err := task.Run(context.Background())

	// After our fix, it should succeed and store the async operation
	require.NoError(t, err, "Should succeed even when GetOperations fails")

	// Check that the rule was updated with success status
	require.Len(t, mockHandler.updatedObjects, 1, "Rule should have been updated exactly once")

	// Check that the rule shows success and has the async operation stored
	updatedRule, ok := mockHandler.updatedObjects[0].(*v1.SummaryRule)
	require.True(t, ok, "Updated object should be a SummaryRule")
	require.Equal(t, ruleName, updatedRule.Name, "Rule name should match")

	// Verify the condition shows success
	condition := updatedRule.GetCondition()
	require.NotNil(t, condition, "Rule should have a condition")
	require.Equal(t, metav1.ConditionTrue, condition.Status, "Condition status should be True")
	require.Empty(t, condition.Message, "Condition message should be empty for success")

	// Should have one async operation since submission succeeded
	asyncOps := updatedRule.GetAsyncOperations()
	require.Len(t, asyncOps, 1, "Should have one async operation when submission succeeds")
	require.Equal(t, "operation-id-123", asyncOps[0].OperationId, "Should have the correct operation ID")
}

func TestSummaryRuleGetOperationsSucceedsAfterFailure(t *testing.T) {
	// This test ensures that the system can recover when GetOperations initially fails
	// but then succeeds in a subsequent run, properly handling existing operations

	// Create a mock statement executor
	mockExecutor := &TestStatementExecutor{
		database: "testdb",
		endpoint: "http://test-endpoint",
	}

	// Create a summary rule
	ruleName := "test-rule"
	rule := &v1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: ruleName,
		},
		Spec: v1.SummaryRuleSpec{
			Database: "testdb",
			Table:    "TestTable",
			Interval: metav1.Duration{Duration: time.Hour},
			Body:     "TestBody",
		},
	}

	// Create a list to be returned by the mock handler
	ruleList := &v1.SummaryRuleList{
		Items: []v1.SummaryRule{*rule},
	}

	// Create a mock handler that will return our rule and track updates
	mockHandler := &mockCRDHandler{
		listResponse:   ruleList,
		updatedObjects: []client.Object{},
	}

	// Create the task with our mocks
	task := &SummaryRuleTask{
		store:    mockHandler,
		kustoCli: mockExecutor,
	}

	// Track GetOperations call count to simulate initial failure then success
	getOperationsCallCount := 0
	task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
		getOperationsCallCount++
		if getOperationsCallCount == 1 {
			// First call fails (simulating Kusto unavailable)
			return nil, errors.New("kusto connection failed")
		}
		// Second call succeeds but returns empty list (no operations)
		return []AsyncOperationStatus{}, nil
	}

	// Mock the SubmitRule function
	submitRuleCallCount := 0
	task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
		submitRuleCallCount++
		return fmt.Sprintf("operation-id-%d", submitRuleCallCount), nil
	}

	// First run - GetOperations fails but rule processing should continue with our fix
	err := task.Run(context.Background())
	require.NoError(t, err, "Should succeed even when GetOperations fails")
	require.Equal(t, 1, getOperationsCallCount, "GetOperations should have been called once")
	require.Equal(t, 1, submitRuleCallCount, "SubmitRule should have been called once")

	// Verify rule was updated with the new operation
	require.Len(t, mockHandler.updatedObjects, 1, "Rule should have been updated once")
	updatedRule1, ok := mockHandler.updatedObjects[0].(*v1.SummaryRule)
	require.True(t, ok, "Updated object should be a SummaryRule")
	asyncOps1 := updatedRule1.GetAsyncOperations()
	require.Len(t, asyncOps1, 1, "Should have one async operation from first run")
	require.Equal(t, "operation-id-1", asyncOps1[0].OperationId, "Should have the operation from first run")

	// Reset mock handler for second run
	mockHandler.updatedObjects = []client.Object{}

	// Second run - GetOperations succeeds
	err = task.Run(context.Background())
	require.NoError(t, err, "Should succeed when GetOperations succeeds")
	require.Equal(t, 2, getOperationsCallCount, "GetOperations should have been called twice")

	// The key test: this should work fine even after the initial GetOperations failure
	// The exact behavior (whether new operations are created) depends on timing logic,
	// but the main point is that the system doesn't crash and continues to function
}

func TestSummaryRuleGetOperationsFailureWithExistingOperations(t *testing.T) {
	// This test ensures that when GetOperations fails but we have existing async operations
	// stored in the CRD, they are still handled correctly (removed if old enough)

	// Create a mock statement executor
	mockExecutor := &TestStatementExecutor{
		database: "testdb",
		endpoint: "http://test-endpoint",
	}

	// Create a summary rule with an old async operation that should be removed
	ruleName := "test-rule"
	oldOperationId := "old-op-123"
	rule := &v1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: ruleName,
		},
		Spec: v1.SummaryRuleSpec{
			Database: "testdb",
			Table:    "TestTable",
			Interval: metav1.Duration{Duration: time.Hour},
			Body:     "TestBody",
		},
	}

	// Add an old async operation (older than 25 hours, should be removed)
	oldStartTime := time.Now().Add(-26 * time.Hour).Format(time.RFC3339Nano)
	rule.SetAsyncOperation(v1.AsyncOperation{
		OperationId: oldOperationId,
		StartTime:   oldStartTime,
		EndTime:     time.Now().Add(-25 * time.Hour).Format(time.RFC3339Nano),
	})

	// Create a list to be returned by the mock handler
	ruleList := &v1.SummaryRuleList{
		Items: []v1.SummaryRule{*rule},
	}

	// Create a mock handler that will return our rule and track updates
	mockHandler := &mockCRDHandler{
		listResponse:   ruleList,
		updatedObjects: []client.Object{},
	}

	// Create the task with our mocks
	task := &SummaryRuleTask{
		store:    mockHandler,
		kustoCli: mockExecutor,
	}

	// Set the GetOperations function to return an error (Kusto unavailable)
	getOperationsError := errors.New("failed to connect to Kusto cluster")
	task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
		return nil, getOperationsError
	}

	// Mock the SubmitRule function to succeed
	task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
		return "new-operation-id-456", nil
	}

	// Run the task - should succeed despite GetOperations failure
	err := task.Run(context.Background())
	require.NoError(t, err, "Should succeed even when GetOperations fails")

	// Check that the rule was updated
	require.Len(t, mockHandler.updatedObjects, 1, "Rule should have been updated once")

	// Check that the old async operation was removed and a new one was created
	updatedRule, ok := mockHandler.updatedObjects[0].(*v1.SummaryRule)
	require.True(t, ok, "Updated object should be a SummaryRule")
	require.Equal(t, ruleName, updatedRule.Name, "Rule name should match")

	// The old operation should be removed due to age, and a new one should be created
	asyncOps := updatedRule.GetAsyncOperations()
	require.Len(t, asyncOps, 1, "Should have one async operation (the new one)")
	require.Equal(t, "new-operation-id-456", asyncOps[0].OperationId, "Should be the new operation")
	require.NotEqual(t, oldOperationId, asyncOps[0].OperationId, "Old operation should be gone due to age")
}

func TestSummaryRuleGetOperationsFailureWithRecentOperations(t *testing.T) {
	// This test ensures that when GetOperations fails but we have recent async operations
	// stored in the CRD, they are kept (not removed due to age)

	// Create a mock statement executor
	mockExecutor := &TestStatementExecutor{
		database: "testdb",
		endpoint: "http://test-endpoint",
	}

	// Create a summary rule with a recent async operation that should be kept
	ruleName := "test-rule"
	recentOperationId := "recent-op-123"
	rule := &v1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: ruleName,
		},
		Spec: v1.SummaryRuleSpec{
			Database: "testdb",
			Table:    "TestTable",
			Interval: metav1.Duration{Duration: time.Hour},
			Body:     "TestBody",
		},
	}

	// Add a recent async operation (less than 25 hours old, should be kept)
	recentStartTime := time.Now().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	rule.SetAsyncOperation(v1.AsyncOperation{
		OperationId: recentOperationId,
		StartTime:   recentStartTime,
		EndTime:     time.Now().Add(-1 * time.Hour).Format(time.RFC3339Nano),
	})

	// Create a list to be returned by the mock handler
	ruleList := &v1.SummaryRuleList{
		Items: []v1.SummaryRule{*rule},
	}

	// Create a mock handler that will return our rule and track updates
	mockHandler := &mockCRDHandler{
		listResponse:   ruleList,
		updatedObjects: []client.Object{},
	}

	// Create the task with our mocks
	task := &SummaryRuleTask{
		store:    mockHandler,
		kustoCli: mockExecutor,
	}

	// Set the GetOperations function to return an error (Kusto unavailable)
	getOperationsError := errors.New("failed to connect to Kusto cluster")
	task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
		return nil, getOperationsError
	}

	// Mock the SubmitRule function to succeed
	task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
		return "new-operation-id-789", nil
	}

	// Run the task - should succeed despite GetOperations failure
	err := task.Run(context.Background())
	require.NoError(t, err, "Should succeed even when GetOperations fails")

	// Check that the rule was updated
	require.Len(t, mockHandler.updatedObjects, 1, "Rule should have been updated once")

	// Check the operations: recent one should be kept and a new one should be created
	updatedRule, ok := mockHandler.updatedObjects[0].(*v1.SummaryRule)
	require.True(t, ok, "Updated object should be a SummaryRule")
	require.Equal(t, ruleName, updatedRule.Name, "Rule name should match")

	// Should have both operations: the recent one (kept) and the new one
	asyncOps := updatedRule.GetAsyncOperations()
	require.Len(t, asyncOps, 2, "Should have two async operations (recent + new)")

	// Check that both operations are present
	operationIds := []string{asyncOps[0].OperationId, asyncOps[1].OperationId}
	require.Contains(t, operationIds, recentOperationId, "Should keep the recent operation")
	require.Contains(t, operationIds, "new-operation-id-789", "Should have the new operation")
}

func TestSummaryRuleKustoErrorParsing(t *testing.T) {
	t.Run("update status with non-kusto error", func(t *testing.T) {
		mockHandler := &mockCRDHandler{
			updatedObjects: make([]client.Object, 0),
		}

		rule := &v1.SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-rule",
				Namespace: "default",
			},
			Spec: v1.SummaryRuleSpec{
				Database: "testdb",
				Table:    "testtable",
				Name:     "test-rule",
				Body:     "test query",
				Interval: metav1.Duration{Duration: time.Minute},
			},
		}

		task := &SummaryRuleTask{store: mockHandler}
		require.NoError(t, task.updateSummaryRuleStatus(context.Background(), rule, io.EOF))

		// Check that the condition was set with the raw error message
		condition := rule.GetCondition()
		require.NotNil(t, condition, "Condition should be set")
		require.Equal(t, metav1.ConditionFalse, condition.Status, "Status should be False for error")
		require.Equal(t, io.EOF.Error(), condition.Message, "Message should be the raw error")

		// Test truncation for long error messages
		longError := errors.New(strings.Repeat("a", 300))
		require.NoError(t, task.updateSummaryRuleStatus(context.Background(), rule, longError))

		condition = rule.GetCondition()
		require.NotNil(t, condition, "Condition should be set")
		require.Equal(t, metav1.ConditionFalse, condition.Status, "Status should be False for error")
		require.Equal(t, strings.Repeat("a", 256), condition.Message, "Message should be truncated to 256 chars")
	})

	t.Run("update status with kusto-http error", func(t *testing.T) {
		mockHandler := &mockCRDHandler{
			updatedObjects: make([]client.Object, 0),
		}

		rule := &v1.SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-rule",
				Namespace: "default",
			},
			Spec: v1.SummaryRuleSpec{
				Database: "testdb",
				Table:    "testtable",
				Name:     "test-rule",
				Body:     "test query",
				Interval: metav1.Duration{Duration: time.Minute},
			},
		}

		// Create a Kusto HTTP error with structured message
		body := `{"error":{"@message": "query contains invalid syntax"}}`
		kustoErr := kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		task := &SummaryRuleTask{store: mockHandler}
		require.NoError(t, task.updateSummaryRuleStatus(context.Background(), rule, kustoErr))

		// Check that the condition was set with the parsed Kusto error message
		condition := rule.GetCondition()
		require.NotNil(t, condition, "Condition should be set")
		require.Equal(t, metav1.ConditionFalse, condition.Status, "Status should be False for error")
		require.Equal(t, "query contains invalid syntax", condition.Message, "Message should be parsed from Kusto error")

		// Test truncation for long Kusto error messages
		longMsg := strings.Repeat("b", 300)
		body = fmt.Sprintf(`{"error":{"@message": "%s"}}`, longMsg)
		kustoErr = kustoerrors.HTTP(kustoerrors.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")
		require.NoError(t, task.updateSummaryRuleStatus(context.Background(), rule, kustoErr))

		condition = rule.GetCondition()
		require.NotNil(t, condition, "Condition should be set")
		require.Equal(t, metav1.ConditionFalse, condition.Status, "Status should be False for error")
		require.Equal(t, longMsg[:256], condition.Message, "Message should be truncated to 256 chars")
	})

	t.Run("update status without error", func(t *testing.T) {
		mockHandler := &mockCRDHandler{
			updatedObjects: make([]client.Object, 0),
		}

		rule := &v1.SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-rule",
				Namespace: "default",
			},
			Spec: v1.SummaryRuleSpec{
				Database: "testdb",
				Table:    "testtable",
				Name:     "test-rule",
				Body:     "test query",
				Interval: metav1.Duration{Duration: time.Minute},
			},
		}

		task := &SummaryRuleTask{store: mockHandler}
		require.NoError(t, task.updateSummaryRuleStatus(context.Background(), rule, nil))

		// Check that the condition was set with success status
		condition := rule.GetCondition()
		require.NotNil(t, condition, "Condition should be set")
		require.Equal(t, metav1.ConditionTrue, condition.Status, "Status should be True for success")
		require.Empty(t, condition.Message, "Message should be empty for success")
	})
}

func TestSummaryRules(t *testing.T) {
	testutils.IntegrationTest(t)

	scheme := clientgoscheme.Scheme
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, adxmonv1.AddToScheme(scheme))

	ctx := context.Background()
	k3sContainer, err := k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	testcontainers.CleanupContainer(t, k3sContainer)
	require.NoError(t, err)

	require.NoError(t, testutils.InstallCrds(ctx, k3sContainer))

	kustoContainer, err := kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithCluster(ctx, k3sContainer))
	testcontainers.CleanupContainer(t, kustoContainer)
	require.NoError(t, err)

	restConfig, ctrlCli, err := testutils.GetKubeConfig(ctx, k3sContainer)
	require.NoError(t, err)
	require.NoError(t, kustoContainer.PortForward(ctx, restConfig))

	cb := kusto.NewConnectionStringBuilder(kustoContainer.ConnectionUrl())
	kustoClient, err := kusto.New(cb)
	require.NoError(t, err)
	defer kustoClient.Close()

	databaseName := "NetDefaultDB"

	// Create a source table
	stmt := kql.New(".create table Source (Timestamp:datetime, col1: string, Value: int)")
	_, err = kustoClient.Mgmt(ctx, databaseName, stmt)
	require.NoError(t, err)

	// Ingest some rows
	for i := 0; i < 100; i++ {
		stmt = kql.New(".ingest inline into table Source <| ").AddUnsafe(fmt.Sprintf("%s,a,%d", time.Now().Add(-time.Duration(i)*time.Minute).Format(time.RFC3339), i))
		_, err = kustoClient.Mgmt(ctx, databaseName, stmt)
		require.NoError(t, err)
	}

	executor := &KustoStatementExecutor{
		database: databaseName,
		client:   kustoClient,
	}

	store := storage.NewCRDHandler(ctrlCli, nil)
	task := NewSummaryRuleTask(store, executor, nil)

	resourceName := "testtest"
	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	// Create a SummaryRule
	rule := &adxmonv1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      typeNamespacedName.Name,
			Namespace: typeNamespacedName.Namespace,
		},
		Spec: adxmonv1.SummaryRuleSpec{
			Database: databaseName,
			Table:    "Destination",
			Interval: metav1.Duration{Duration: time.Hour},
			Body:     "Source | where Timestamp between( _startTime .. _endTime) | summarize Avg = avg(Value) by bin(Timestamp, 5m)",
		},
	}
	require.NoError(t, ctrlCli.Create(ctx, rule))

	// Execute the rule
	require.NoError(t, task.Run(ctx))

	// Verify the rule's condition
	rule = &adxmonv1.SummaryRule{}
	require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, rule))
	cnd := rule.GetCondition()
	require.NotNil(t, cnd)
	require.Equal(t, metav1.ConditionTrue, cnd.Status)
	require.NotZero(t, cnd.LastTransitionTime)

	// Wait for the result table
	require.Eventually(t, func() bool {
		return testutils.TableExists(ctx, t, executor.Database(), "Destination", kustoContainer.ConnectionUrl())
	}, 10*time.Minute, time.Second)

	// Executing the rule again should have no effect because
	// the interval hasn't yet elapsed
	require.NoError(t, task.Run(ctx))
	rule = &adxmonv1.SummaryRule{}
	require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, rule))
	nextCnd := rule.GetCondition()
	require.NotNil(t, nextCnd)
	require.Equal(t, cnd.LastTransitionTime, nextCnd.LastTransitionTime)

	// Verify async operation handling
	asyncOps := rule.GetAsyncOperations()
	require.Equal(t, 1, len(asyncOps))

	// Wait for the rule to execute in Kusto
	require.Eventually(t, func() bool {
		ops, err := task.GetOperations(ctx)
		if err != nil {
			return false
		}
		for _, op := range ops {
			if op.OperationId == asyncOps[0].OperationId {
				return IsKustoAsyncOperationStateCompleted(op.State)
			}
		}
		return false
	}, 10*time.Minute, time.Second)

	// Executing the rule should remove the succeeded async operation
	require.NoError(t, task.Run(ctx))
	require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, rule))
	nextCnd = rule.GetCondition()
	require.NotNil(t, nextCnd)
	asyncOps = rule.GetAsyncOperations()
	require.Equal(t, 0, len(asyncOps))

	t.Run("Offline operation", func(t *testing.T) {
		// Move back in time to ensure our predicate thinks we need to fill a backlog
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, rule))
		resetToTime := rule.GetLastExecutionTime().Add(-time.Hour * 10)
		rule.SetLastExecutionTime(resetToTime)
		require.NoError(t, ctrlCli.Status().Update(ctx, rule))

		// Simulate failures
		task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
			return "", errors.New("some failure scenario")
		}

		// Execute the rule, we expect to accumulate operations
		for range 3 {
			require.NoError(t, task.Run(ctx))
		}
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, rule))
		ops := rule.GetAsyncOperations()
		require.Equal(t, 3, len(ops), "Should have 3 async operations due to failures")

		for _, op := range ops {
			require.Empty(t, op.OperationId, "Cluster is offline so our operation-id should not be set")
		}

		// Simulate cluster availability
		task.SubmitRule = task.submitRule

		// Submit the backlog
		require.NoError(t, task.Run(ctx))
		require.NoError(t, ctrlCli.Get(ctx, typeNamespacedName, rule))

		for _, op := range rule.GetAsyncOperations() {
			require.NotEmpty(t, op.OperationId, "Cluster is online so backlog operation-ids should be set")
		}
	})
}

func TestSummaryRuleSubmissionFailureDoesNotCauseImmediateRetry(t *testing.T) {
	// This test validates the fix for issue #796 - ensures that submission failures
	// don't cause immediate retries on the next execution cycle due to ConditionFalse status.

	// Create a mock statement executor
	mockExecutor := &TestStatementExecutor{
		database: "testdb",
		endpoint: "http://test-endpoint",
	}

	// Create a summary rule with a long interval to ensure no time-based retries
	ruleName := "test-rule"
	rule := &v1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: ruleName,
		},
		Spec: v1.SummaryRuleSpec{
			Database: "testdb",
			Table:    "TestTable",
			Interval: metav1.Duration{Duration: time.Hour}, // Long interval
			Body:     "TestBody",
		},
	}

	// Create a list to be returned by the mock handler
	ruleList := &v1.SummaryRuleList{
		Items: []v1.SummaryRule{*rule},
	}

	// Create a mock handler that will return our rule and track updates
	mockHandler := &mockCRDHandler{
		listResponse:   ruleList,
		updatedObjects: []client.Object{},
	}

	// Create the task with our mocks
	task := &SummaryRuleTask{
		store:    mockHandler,
		kustoCli: mockExecutor,
	}

	// Set the GetOperations function to return an empty list
	task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
		return []AsyncOperationStatus{}, nil
	}

	// Track submission calls - should fail consistently to prevent backlog recovery
	submitCallCount := 0
	task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
		submitCallCount++
		// Always fail to prevent the backlog mechanism from succeeding
		return "", errors.New("kusto connection failed")
	}

	// First run - submission fails, status should become False
	err := task.Run(context.Background())
	require.NoError(t, err, "Task should succeed even when submission fails")

	// The first run might call SubmitRule twice:
	// 1. Initial submission for new window
	// 2. Backlog retry for the failed operation with empty OperationId
	// This is expected behavior of the backlog mechanism
	firstRunCalls := submitCallCount
	require.GreaterOrEqual(t, firstRunCalls, 1, "SubmitRule should have been called at least once")

	// Verify the rule was updated with failure status
	require.Len(t, mockHandler.updatedObjects, 1, "Rule should have been updated once")
	updatedRule1, ok := mockHandler.updatedObjects[0].(*v1.SummaryRule)
	require.True(t, ok, "Updated object should be a SummaryRule")

	condition1 := updatedRule1.GetCondition()
	require.NotNil(t, condition1, "Rule should have a condition")
	require.Equal(t, metav1.ConditionFalse, condition1.Status, "Condition status should be False after failure")

	// Reset the updated objects list for the second run
	mockHandler.updatedObjects = []client.Object{}

	// Update the rule list to use the failed rule for the second run
	ruleList.Items[0] = *updatedRule1

	// Second run immediately after - should NOT create NEW submissions due to ConditionFalse
	// It may still retry backlog operations (which is expected), but shouldn't create new operations
	err = task.Run(context.Background())
	require.NoError(t, err, "Task should succeed on second run")

	secondRunCalls := submitCallCount - firstRunCalls

	// Key assertion: The fix ensures that we don't create NEW submissions just because status is False.
	// Any additional calls should be from backlog processing, not from the shouldSubmitRule logic.
	// Since the interval is 1 hour and we're running immediately, there should be no time-based retries.
	// The only retries should be from the backlog mechanism trying to recover the failed operations.

	// To validate the fix, we check that the number of calls in the second run is not greater than
	// the number of failed operations from the first run (which would be retried via backlog).
	asyncOps := updatedRule1.GetAsyncOperations()
	expectedBacklogRetries := 0
	for _, op := range asyncOps {
		if op.OperationId == "" {
			expectedBacklogRetries++
		}
	}

	require.LessOrEqual(t, secondRunCalls, expectedBacklogRetries,
		"Second run should only retry backlog operations, not create new ones due to ConditionFalse")
}

var severalFunctions = `// function a
.create-or-alter function a() {
  print "a"
}
//
.create-or-alter function b() {
  print "b"
}`

func TestApplySubstitutions(t *testing.T) {
	tests := []struct {
		Name          string
		RuleBody      string
		ClusterLabels map[string]string
		Want          string
	}{
		{
			Name: "timestamps",
			RuleBody: `T
| where Timestamp between( _startTime .. _endTime )`,
			Want: `let _startTime=datetime(2024-01-01T00:00:00Z);
let _endTime=datetime(2024-01-01T01:00:00Z);
T
| where Timestamp between( _startTime .. _endTime )`,
		},
		{
			Name: "region",
			RuleBody: `T
| where Timestamp between( _startTime .. _endTime )
| where Region == _region`,
			ClusterLabels: map[string]string{
				"_region": "eastus",
			},
			Want: `let _startTime=datetime(2024-01-01T00:00:00Z);
let _endTime=datetime(2024-01-01T01:00:00Z);
let _region="eastus";
T
| where Timestamp between( _startTime .. _endTime )
| where Region == _region`,
		},
		{
			Name: "region and environment",
			RuleBody: `T
| where Timestamp between( _startTime .. _endTime )
| where Region == _region
| where Environment != _environment`,
			ClusterLabels: map[string]string{
				"_region":      "eastus",
				"_environment": "production",
			},
			Want: `let _startTime=datetime(2024-01-01T00:00:00Z);
let _endTime=datetime(2024-01-01T01:00:00Z);
let _environment="production";
let _region="eastus";
T
| where Timestamp between( _startTime .. _endTime )
| where Region == _region
| where Environment != _environment`,
		},
	}
	startTime := "2024-01-01T00:00:00Z"
	endTime := "2024-01-01T01:00:00Z"
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			have := applySubstitutions(tt.RuleBody, startTime, endTime, tt.ClusterLabels)
			require.Equal(t, tt.Want, have)
		})
	}
}

func TestSummaryRuleCriteriaMatching(t *testing.T) {
	tests := []struct {
		name          string
		criteria      map[string][]string
		clusterLabels map[string]string
		shouldMatch   bool
		description   string
	}{
		{
			name:        "no criteria - should always match",
			criteria:    nil,
			shouldMatch: true,
			description: "Rules with no criteria should always execute",
		},
		{
			name:        "empty criteria - should always match",
			criteria:    map[string][]string{},
			shouldMatch: true,
			description: "Rules with empty criteria should always execute",
		},
		{
			name: "exact match - single value",
			criteria: map[string][]string{
				"region": {"eastus"},
			},
			clusterLabels: map[string]string{
				"region": "eastus",
			},
			shouldMatch: true,
			description: "Rule should match when cluster has the exact required label value",
		},
		{
			name: "case insensitive match - single value",
			criteria: map[string][]string{
				"region": {"EastUS"},
			},
			clusterLabels: map[string]string{
				"REGION": "eastus",
			},
			shouldMatch: true,
			description: "Rule should match case-insensitively",
		},
		{
			name: "no match - different value",
			criteria: map[string][]string{
				"region": {"eastus"},
			},
			clusterLabels: map[string]string{
				"region": "westus",
			},
			shouldMatch: false,
			description: "Rule should not match when cluster has different label value",
		},
		{
			name: "no match - missing label",
			criteria: map[string][]string{
				"region": {"eastus"},
			},
			clusterLabels: map[string]string{
				"environment": "production",
			},
			shouldMatch: false,
			description: "Rule should not match when cluster is missing required label",
		},
		{
			name: "match - multiple values (OR logic)",
			criteria: map[string][]string{
				"region": {"eastus", "westus"},
			},
			clusterLabels: map[string]string{
				"region": "westus",
			},
			shouldMatch: true,
			description: "Rule should match when cluster has any of the specified values (OR logic)",
		},
		{
			name: "match - multiple criteria (any match)",
			criteria: map[string][]string{
				"region":      {"eastus"},
				"environment": {"staging"},
			},
			clusterLabels: map[string]string{
				"region":      "westus",  // doesn't match
				"environment": "staging", // matches
			},
			shouldMatch: true,
			description: "Rule should match when any criteria matches (OR logic between criteria)",
		},
		{
			name: "no match - multiple criteria (no match)",
			criteria: map[string][]string{
				"region":      {"eastus"},
				"environment": {"staging"},
			},
			clusterLabels: map[string]string{
				"region":      "westus",     // doesn't match
				"environment": "production", // doesn't match
			},
			shouldMatch: false,
			description: "Rule should not match when no criteria matches",
		},
		{
			name: "match - multiple environments (OR logic)",
			criteria: map[string][]string{
				"environment": {"production", "staging", "development"},
			},
			clusterLabels: map[string]string{
				"environment": "staging",
			},
			shouldMatch: true,
			description: "Rule should match when cluster has any of the specified environment values (OR logic)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the criteria matching logic directly
			shouldExecute := matchesCriteria(tt.criteria, tt.clusterLabels)
			require.Equal(t, tt.shouldMatch, shouldExecute, tt.description)
		})
	}
}

func TestSummaryRuleDoubleExecutionFix(t *testing.T) {
	// Test that submitting a rule doesn't cause double execution across multiple execution cycles
	// The fix ensures that completed operations (with ShouldRetry=0) are not processed for retry
	rule := &v1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-rule",
			Generation: 1,
		},
		Spec: v1.SummaryRuleSpec{
			Database: "testdb",
			Table:    "TestTable",
			Interval: metav1.Duration{Duration: time.Minute}, // Use 1 minute for faster testing
			Body:     "TestBody",
		},
	}

	mockHandler := &mockCRDHandler{
		listResponse: &v1.SummaryRuleList{Items: []v1.SummaryRule{*rule}},
	}

	mockExecutor := &TestStatementExecutor{
		database: "testdb",
		endpoint: "http://test-endpoint",
	}

	task := &SummaryRuleTask{
		store:    mockHandler,
		kustoCli: mockExecutor,
	}

	var submitCount int
	var allSubmittedOperations []string

	task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
		submitCount++
		operationId := fmt.Sprintf("op-%d", submitCount)
		allSubmittedOperations = append(allSubmittedOperations, operationId)
		t.Logf("SubmitRule called #%d, operationId: %s", submitCount, operationId)
		return operationId, nil
	}

	// Mock GetOperations to return all previously submitted operations as completed
	task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
		var operations []AsyncOperationStatus
		for _, opId := range allSubmittedOperations {
			operations = append(operations, AsyncOperationStatus{
				OperationId: opId,
				State:       string(KustoAsyncOperationStateCompleted),
				ShouldRetry: 0, // Completed operations should have ShouldRetry=0
			})
		}
		return operations, nil
	}

	// Test multiple execution cycles
	for cycle := 1; cycle <= 3; cycle++ {
		t.Logf("Running execution cycle %d", cycle)

		initialSubmitCount := submitCount
		err := task.Run(context.Background())
		require.NoError(t, err)

		// Each cycle should submit exactly one operation (no double execution)
		expectedSubmitCount := initialSubmitCount + 1
		require.Equal(t, expectedSubmitCount, submitCount,
			"Cycle %d: Rule should be submitted only once per cycle", cycle)

		// Simulate time advancement by updating the rule's last successful execution time
		// This ensures the next cycle will be eligible for execution
		if cycle < 3 { // Don't advance time after the last cycle
			newEndTime := time.Now().UTC().Add(time.Duration(cycle) * time.Minute)
			rule.SetLastExecutionTime(newEndTime)
		}
	}

	require.Equal(t, 3, submitCount, "Should have submitted exactly 3 operations across 3 cycles")
}

func TestSummaryRuleHandlesMixedAsyncOperationStatesCorrectly(t *testing.T) {
	// This test simulates a bug scenario where there are multiple outstanding async operations:
	// - First operation: Failed state but marked for retry (ShouldRetry=1)
	// - Subsequent operations: Completed state (ShouldRetry=0)
	// Expected behavior:
	// - Failed operation should be retried (new operation created)
	// - All Completed operations should be removed
	// - Final state: Operations from both new rule execution and retry

	// Create a mock statement executor
	mockExecutor := &TestStatementExecutor{
		database: "testdb",
		endpoint: "http://test-endpoint",
	}

	// Create a summary rule that already has multiple async operations
	ruleName := "test-rule"
	rule := &v1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:       ruleName,
			Generation: 1,
		},
		Spec: v1.SummaryRuleSpec{
			Database: "testdb",
			Table:    "TestTable",
			Interval: metav1.Duration{Duration: time.Hour},
			Body:     "TestBody",
		},
	}

	// Setup initial async operations - simulating the buggy production state
	// First operation: Failed but retriable
	failedOp := v1.AsyncOperation{
		OperationId: "failed-op-1",
		StartTime:   "2024-06-23T10:00:00Z",
		EndTime:     "2024-06-23T11:00:00Z",
	}
	rule.SetAsyncOperation(failedOp)

	// Subsequent operations: All completed successfully
	completedOps := []v1.AsyncOperation{
		{
			OperationId: "completed-op-2",
			StartTime:   "2024-06-23T11:00:00Z",
			EndTime:     "2024-06-23T12:00:00Z",
		},
		{
			OperationId: "completed-op-3",
			StartTime:   "2024-06-23T12:00:00Z",
			EndTime:     "2024-06-23T13:00:00Z",
		},
		{
			OperationId: "completed-op-4",
			StartTime:   "2024-06-23T13:00:00Z",
			EndTime:     "2024-06-23T14:00:00Z",
		},
	}

	for _, op := range completedOps {
		rule.SetAsyncOperation(op)
	}

	// Verify initial state - should have 4 operations total
	initialOps := rule.GetAsyncOperations()
	require.Len(t, initialOps, 4, "Should start with 4 async operations")

	// Create a list to be returned by the mock handler
	ruleList := &v1.SummaryRuleList{
		Items: []v1.SummaryRule{*rule},
	}

	// Create a mock handler that will return our rule and track updates
	mockHandler := &mockCRDHandler{
		listResponse:   ruleList,
		updatedObjects: []client.Object{},
	}

	// Create the task with our mocks
	task := &SummaryRuleTask{
		store:    mockHandler,
		kustoCli: mockExecutor,
	}

	// Mock GetOperations to return the mixed states from Kusto
	task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
		return []AsyncOperationStatus{
			{
				OperationId: "failed-op-1",
				State:       string(KustoAsyncOperationStateFailed),
				ShouldRetry: 1, // This is the key - Failed but retriable
			},
			{
				OperationId: "completed-op-2",
				State:       string(KustoAsyncOperationStateCompleted),
				ShouldRetry: 0, // Completed successfully
			},
			{
				OperationId: "completed-op-3",
				State:       string(KustoAsyncOperationStateCompleted),
				ShouldRetry: 0, // Completed successfully
			},
			{
				OperationId: "completed-op-4",
				State:       string(KustoAsyncOperationStateCompleted),
				ShouldRetry: 0, // Completed successfully
			},
		}, nil
	}
	// Track SubmitRule calls - we expect both new execution and retry
	type submitCall struct {
		startTime string
		endTime   string
		opId      string
	}
	var submitCalls []submitCall

	task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
		opId := fmt.Sprintf("op-%d", len(submitCalls)+1)
		submitCalls = append(submitCalls, submitCall{
			startTime: startTime,
			endTime:   endTime,
			opId:      opId,
		})
		return opId, nil
	}

	// Execute the task
	err := task.Run(context.Background())
	require.NoError(t, err, "Task should execute successfully")

	// We expect 2 SubmitRule calls:
	// 1. New rule execution (for current time window)
	// 2. Retry of failed operation (with original time window)
	require.Len(t, submitCalls, 2, "Should have 2 SubmitRule calls: new execution + retry")

	// Find the retry call - it should match the failed operation's time window
	var retryCall *submitCall
	for i := range submitCalls {
		if submitCalls[i].startTime == "2024-06-23T10:00:00Z" && submitCalls[i].endTime == "2024-06-23T11:00:00Z" {
			retryCall = &submitCalls[i]
			break
		}
	}
	require.NotNil(t, retryCall, "Should have a retry call with the failed operation's time window")

	// Verify the rule was updated (could be multiple times due to both new execution and retries)
	require.GreaterOrEqual(t, len(mockHandler.updatedObjects), 1, "Rule should have been updated at least once")

	// Get the final updated rule
	updatedRule, ok := mockHandler.updatedObjects[len(mockHandler.updatedObjects)-1].(*v1.SummaryRule)
	require.True(t, ok, "Updated object should be a SummaryRule")
	require.Equal(t, ruleName, updatedRule.Name, "Rule name should match")

	// Critical verification: All completed operations should be removed
	finalOps := updatedRule.GetAsyncOperations()

	// Verify none of the completed operations remain
	for _, completedOp := range completedOps {
		for _, finalOp := range finalOps {
			require.NotEqual(t, completedOp.OperationId, finalOp.OperationId,
				"Completed operation %s should have been removed", completedOp.OperationId)
		}
	}

	// Verify the original failed operation was removed
	for _, finalOp := range finalOps {
		require.NotEqual(t, "failed-op-1", finalOp.OperationId, "Original failed operation should have been removed")
	}

	// Verify that the retry operation is present
	retryOpFound := false
	for _, finalOp := range finalOps {
		if finalOp.OperationId == retryCall.opId {
			retryOpFound = true
			require.Equal(t, "2024-06-23T10:00:00Z", finalOp.StartTime, "Retry operation should have correct start time")
			require.Equal(t, "2024-06-23T11:00:00Z", finalOp.EndTime, "Retry operation should have correct end time")
			break
		}
	}
	require.True(t, retryOpFound, "Retry operation should be present in final operations")
}

// mockStatementExecutor is a test double for StatementExecutor
type mockStatementExecutor struct {
	database string
	endpoint string
	mgmtFunc func(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error)
}

func (m *mockStatementExecutor) Database() string {
	return m.database
}

func (m *mockStatementExecutor) Endpoint() string {
	return m.endpoint
}

func (m *mockStatementExecutor) Mgmt(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
	if m.mgmtFunc != nil {
		return m.mgmtFunc(ctx, query, options...)
	}
	return &kusto.RowIterator{}, nil
}

// createMockRowIterator creates a mock RowIterator for testing TableDetail results
func createMockRowIterator(tables []TableDetail) *kusto.RowIterator {
	rows, err := kusto.NewMockRows(table.Columns{
		{Name: "TableName", Type: "string"},
		{Name: "HotExtentSize", Type: "real"},
		{Name: "TotalExtentSize", Type: "real"},
		{Name: "TotalExtents", Type: "long"},
		{Name: "HotRowCount", Type: "long"},
		{Name: "TotalRowCount", Type: "long"},
	})
	if err != nil {
		panic(err) // This should never happen in tests
	}

	// Add mock data for each table
	for _, table := range tables {
		rows.Row(value.Values{
			value.String{Value: table.TableName, Valid: true},
			value.Real{Value: table.HotExtentSize, Valid: true},
			value.Real{Value: table.TotalExtentSize, Valid: true},
			value.Long{Value: table.TotalExtents, Valid: true},
			value.Long{Value: table.HotRowCount, Valid: true},
			value.Long{Value: table.TotalRowCount, Valid: true},
		})
	}

	// Signal end of data
	rows.Error(io.EOF)

	iter := &kusto.RowIterator{}
	iter.Mock(rows)
	return iter
}

func TestSummaryRuleConcurrentExecutionProtection(t *testing.T) {
	// Test that concurrent execution of the same rule is prevented
	// and that the IngestorSummaryRuleConcurrentSkipped metric is incremented

	mockHandler := &mockCRDHandler{}

	// Mock StatementExecutor that blocks on the first call
	mockExecutor := &mockStatementExecutor{
		database: "test-db",
		endpoint: "https://test.kusto.windows.net",
	}

	// Create a test rule
	testRule := &v1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rule",
			Namespace: "test-namespace",
		},
		Spec: v1.SummaryRuleSpec{
			Database: "test-db",
			Table:    "test-table",
			Body:     "TestData | count",
			Interval: metav1.Duration{Duration: time.Hour},
		},
	}

	ruleList := &v1.SummaryRuleList{
		Items: []v1.SummaryRule{*testRule},
	}
	mockHandler.listResponse = ruleList

	task := NewSummaryRuleTask(mockHandler, mockExecutor, map[string]string{})

	// Track when the first execution starts and finishes
	firstExecutionStarted := make(chan struct{})
	firstExecutionFinished := make(chan struct{})

	// Mock SubmitRule to block on first call
	callCount := 0
	task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
		callCount++
		if callCount == 1 {
			close(firstExecutionStarted)
			<-firstExecutionFinished // Block until we signal to continue
			return "operation-1", nil
		}
		return "operation-2", nil
	}

	// Mock GetOperations to return empty (no existing operations)
	task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
		return []AsyncOperationStatus{}, nil
	}

	ctx := context.Background()

	// Start first execution in a goroutine
	firstExecutionError := make(chan error, 1)
	go func() {
		err := task.handleRuleExecution(ctx, testRule)
		firstExecutionError <- err
	}()

	// Wait for first execution to start and be blocked
	<-firstExecutionStarted

	// Start second execution - this should be skipped due to concurrent execution protection
	secondExecutionError := make(chan error, 1)
	go func() {
		err := task.handleRuleExecution(ctx, testRule)
		secondExecutionError <- err
	}()

	// Give the second execution a moment to attempt and be blocked
	time.Sleep(100 * time.Millisecond)

	// Release the first execution
	close(firstExecutionFinished)

	// Wait for both executions to complete
	err1 := <-firstExecutionError
	err2 := <-secondExecutionError

	// Both should succeed (second one is skipped but returns nil)
	require.NoError(t, err1, "First execution should succeed")
	require.NoError(t, err2, "Second execution should succeed (but be skipped)")

	// SubmitRule should only have been called once (for the first execution)
	require.Equal(t, 1, callCount, "SubmitRule should only be called once")

	// Verify that the rule was added and removed from the execution map
	// The ruleExecution map should be empty after both executions complete
	executionMapSize := 0
	task.ruleExecution.Range(func(key, value interface{}) bool {
		executionMapSize++
		return true
	})
	require.Equal(t, 0, executionMapSize, "Rule execution map should be empty after completion")
}

func TestSummaryRuleExecutionMapCleanup(t *testing.T) {
	// Test that the execution map is properly cleaned up even if execution fails

	mockHandler := &mockCRDHandler{}
	mockExecutor := &mockStatementExecutor{
		database: "test-db",
		endpoint: "https://test.kusto.windows.net",
	}

	testRule := &v1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rule",
			Namespace: "test-namespace",
		},
		Spec: v1.SummaryRuleSpec{
			Database: "test-db",
			Table:    "test-table",
			Body:     "TestData | count",
			Interval: metav1.Duration{Duration: time.Hour},
		},
	}

	task := NewSummaryRuleTask(mockHandler, mockExecutor, map[string]string{})

	// Mock SubmitRule to return an error
	task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
		return "", errors.New("submission failed")
	}

	// Mock GetOperations to return empty
	task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
		return []AsyncOperationStatus{}, nil
	}

	ctx := context.Background()

	// Execute the rule - this should fail but still clean up
	err := task.handleRuleExecution(ctx, testRule)
	require.Error(t, err, "Execution should fail due to SubmitRule error")

	// Verify that the rule was removed from the execution map even after failure
	executionMapSize := 0
	task.ruleExecution.Range(func(key, value interface{}) bool {
		executionMapSize++
		return true
	})
	require.Equal(t, 0, executionMapSize, "Rule execution map should be empty after failed execution")

	// Verify we can execute the same rule again (not blocked by previous failure)
	task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
		return "operation-1", nil
	}

	err = task.handleRuleExecution(ctx, testRule)
	require.NoError(t, err, "Second execution should succeed")

	executionMapSize = 0
	task.ruleExecution.Range(func(key, value interface{}) bool {
		executionMapSize++
		return true
	})
	require.Equal(t, 0, executionMapSize, "Rule execution map should be empty after successful execution")
}

func TestDropUnusedTablesTaskLockScope(t *testing.T) {
	// Test that loadTableDetails is called outside the mutex lock
	// and concurrent access doesn't block on Kusto operations

	// Create a mock executor that tracks when it's called
	loadDetailsCalled := make(chan struct{}, 1)
	loadDetailsFinished := make(chan struct{})

	mockExecutor := &mockStatementExecutor{
		database: "test-db",
		endpoint: "https://test.kusto.windows.net",
		mgmtFunc: func(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
			// Signal that loadTableDetails was called
			select {
			case loadDetailsCalled <- struct{}{}:
			default:
			}

			// Block until we signal to continue (simulates slow Kusto query)
			<-loadDetailsFinished

			// Return mock table details
			return createMockRowIterator([]TableDetail{
				{TableName: "table1", TotalRowCount: 0},
				{TableName: "table2", TotalRowCount: 100},
			}), nil
		},
	}

	task := NewDropUnusedTablesTask(mockExecutor)

	ctx := context.Background()

	// Start the first Run() call in a goroutine
	firstRunError := make(chan error, 1)
	go func() {
		err := task.Run(ctx)
		firstRunError <- err
	}()

	// Wait for the first Run() to call loadTableDetails
	<-loadDetailsCalled

	// At this point, the first Run() is blocked in loadTableDetails (outside the mutex)
	// Try to access the task's mutex directly to verify it's not held
	mutexAcquired := make(chan bool, 1)
	go func() {
		task.mu.Lock()
		task.mu.Unlock()
		mutexAcquired <- true
	}()

	// This should succeed quickly if the mutex is not held during loadTableDetails
	select {
	case <-mutexAcquired:
		// Good! The mutex was not held during loadTableDetails
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Mutex appears to be held during loadTableDetails - this violates the lock scope fix")
	}

	// Release the first Run() call
	close(loadDetailsFinished)

	// Wait for the first Run() to complete
	err := <-firstRunError
	require.NoError(t, err, "First run should succeed")

	// Verify that the unused table was tracked
	task.mu.Lock()
	require.Contains(t, task.unusedTables, "table1", "table1 should be marked as unused")
	require.Equal(t, 1, task.unusedTables["table1"], "table1 should have count of 1")
	require.NotContains(t, task.unusedTables, "table2", "table2 should not be in unused tables")
	task.mu.Unlock()
}
