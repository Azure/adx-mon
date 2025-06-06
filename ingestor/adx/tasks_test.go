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
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/azure-kusto-go/kusto"
	KERRS "github.com/Azure/azure-kusto-go/kusto/data/errors"
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
	listResponse  client.ObjectList
	listError     error
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
	// Track updated objects for assertions in tests
	m.updatedObjects = append(m.updatedObjects, obj.DeepCopyObject().(client.Object))
	return nil
}

type TestStatementExecutor struct {
	database    string
	endpoint    string
	stmts       []string
	nextMgmtErr error
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
		require.Equal(t, msg[:256], fn.Status.Error)
	})

	t.Run("update status with kusto-http error", func(t *testing.T) {
		fn := &v1.Function{
			Status: v1.FunctionStatus{
				Status: v1.Failed,
			},
		}
		body := `{"error":{"@message": "this function is invalid"}}`
		funcErr := KERRS.HTTP(KERRS.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		task := &SyncFunctionsTask{
			store: &TestFunctionStore{},
		}
		require.NoError(t, task.updateKQLFunctionStatus(context.Background(), fn, v1.Failed, funcErr))
		require.Equal(t, v1.Failed, fn.Status.Status)
		require.Equal(t, "this function is invalid", fn.Status.Error)

		// Requires truncation
		msg := strings.Repeat("a", 300)
		body = fmt.Sprintf(`{"error":{"@message": "%s"}}`, msg)
		funcErr = KERRS.HTTP(KERRS.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")
		require.NoError(t, task.updateKQLFunctionStatus(context.Background(), fn, v1.Failed, funcErr))
		require.Equal(t, v1.Failed, fn.Status.Status)
		require.Equal(t, msg[:256], fn.Status.Error)
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
		listResponse: ruleList,
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
	task := NewSummaryRuleTask(store, executor)

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
}

var severalFunctions = `// function a
.create-or-alter function a() {
  print "a"
}
//
.create-or-alter function b() {
  print "b"
}`
