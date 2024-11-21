package adx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/azure-kusto-go/kusto"
	KERRS "github.com/Azure/azure-kusto-go/kusto/data/errors"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TestStatementExecutor struct {
	database    string
	stmts       []string
	nextMgmtErr error
}

func (t *TestStatementExecutor) Database() string {
	return t.database
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
	statusUpdates []*v1.Function
	nextUpdateErr error
}

func (t *TestFunctionStore) Functions() []*v1.Function {
	ret := make([]*v1.Function, len(t.funcs))
	for i, f := range t.funcs {
		ret[i] = f.DeepCopy()
	}
	return ret
}

func (t *TestFunctionStore) View(database, table string) (*v1.Function, bool) {
	return nil, false
}

func (t *TestFunctionStore) UpdateStatus(ctx context.Context, fn *v1.Function) error {
	t.statusUpdates = append(t.statusUpdates, fn)
	if t.nextUpdateErr != nil {
		ret := t.nextUpdateErr
		t.nextUpdateErr = nil
		return ret
	}
	return nil
}

func TestSyncFunctionsTask(t *testing.T) {
	t.Run("only updates when generation changes", func(t *testing.T) {
		task, fnStore, _, validate := newSyncFunctionsTask(t)
		fnStore.funcs = append(fnStore.funcs, &v1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Generation:      1,
				ResourceVersion: "sdfaasf",
				Name:            "fn2",
				Namespace:       "ns1",
			},
			Spec: v1.FunctionSpec{
				Database: "testdb",
			},
		})
		fnStore.funcs = append(fnStore.funcs, &v1.Function{
			ObjectMeta: metav1.ObjectMeta{
				Generation:      1,
				ResourceVersion: "sdfaasf",
				Name:            "fn2",
				Namespace:       "ns1",
			},
			Spec: v1.FunctionSpec{
				// This is skipped, since it is in a different db.
				Database: "otherdb",
			},
		})

		err := task.Run(context.Background())
		validate(err, 2, 2)

		// Run again, no updates
		err = task.Run(context.Background())
		validate(err, 2, 2)

		fnStore.funcs[0].Generation = 2
		fnStore.nextUpdateErr = io.EOF
		err = task.Run(context.Background())
		validate(err, 3, 3)

		// Run again, updates again because it got an error back attempting to update the status
		err = task.Run(context.Background())
		validate(err, 4, 4)

		// Run again, no updates
		err = task.Run(context.Background())
		validate(err, 4, 4)
	})

	t.Run("handles permanent mgmt errors", func(t *testing.T) {
		task, fnStore, kustoCli, validate := newSyncFunctionsTask(t)
		body := `{"error":{"@message": "this function is invalid"}}`
		kustoCli.nextMgmtErr = KERRS.HTTP(KERRS.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")

		err := task.Run(context.Background())
		validate(err, 1, 1)
		require.Equal(t, "this function is invalid", fnStore.statusUpdates[0].Status.Error)

		// Run again, no updates since the error is permanent.
		err = task.Run(context.Background())
		validate(err, 1, 1)

		fnStore.funcs[0].Generation = 2
		err = task.Run(context.Background())
		// No error from the mgmt call and updated generation, successfully runs
		validate(err, 2, 2)

		// New generation, permanent error but unable to update status
		fnStore.funcs[0].Generation = 3
		body = `{"error":{"@message": "this function is invalid"}}`
		kustoCli.nextMgmtErr = KERRS.HTTP(KERRS.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")
		fnStore.nextUpdateErr = io.EOF
		err = task.Run(context.Background())
		// Attempted to run mgmt and update status
		validate(err, 3, 3)
		require.Equal(t, "this function is invalid", fnStore.statusUpdates[2].Status.Error)

		// Unable to update status so not cached. Try again. Updates status this time.
		body = `{"error":{"@message": "this function is invalid"}}`
		kustoCli.nextMgmtErr = KERRS.HTTP(KERRS.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")
		err = task.Run(context.Background())
		// Attempted to run mgmt and update status
		validate(err, 4, 4)

		// Run again, no updates since the error is permanent.
		err = task.Run(context.Background())
		validate(err, 4, 4)
	})

	t.Run("handles non-permanent mgmt errors", func(t *testing.T) {
		task, fnStore, kustoCli, validate := newSyncFunctionsTask(t)
		body := `{"error":{"@permanent": false}}`
		httperr := KERRS.HTTP(KERRS.OpMgmt, "bad gateway", 502, io.NopCloser(strings.NewReader(body)), "")
		kustoCli.nextMgmtErr = KERRS.E(KERRS.OpMgmt, KERRS.KHTTPError, httperr)

		err := task.Run(context.Background())
		validate(err, 1, 1)
		require.NotEmpty(t, fnStore.statusUpdates[0].Status.Error)

		// Run again and works because the error was transient.
		// Is now successful and cached.
		err = task.Run(context.Background())
		validate(err, 2, 2)

		// Run again, but cached so no updates
		err = task.Run(context.Background())
		validate(err, 2, 2)
	})
}

func TestUpdateKQLFunctionStatus(t *testing.T) {
	t.Run("update status without error", func(t *testing.T) {
		fn := &v1.Function{
			Status: v1.FunctionStatus{
				Status: v1.Failed,
			},
		}
		err := updateKQLFunctionStatus(context.Background(), &TestFunctionStore{}, fn, v1.Success, nil)
		require.NoError(t, err)
		require.Equal(t, v1.Success, fn.Status.Status)
		require.Empty(t, fn.Status.Error)
		require.NotEmpty(t, fn.Status.LastTimeReconciled)
	})

	t.Run("handles error from function store", func(t *testing.T) {
		fn := &v1.Function{
			Status: v1.FunctionStatus{
				Status: v1.Failed,
			},
		}
		err := updateKQLFunctionStatus(context.Background(), &TestFunctionStore{nextUpdateErr: io.EOF}, fn, v1.Success, nil)
		require.Error(t, err)
	})

	t.Run("update status with non-kusto-http error", func(t *testing.T) {
		fn := &v1.Function{
			Status: v1.FunctionStatus{
				Status: v1.Failed,
			},
		}
		err := updateKQLFunctionStatus(context.Background(), &TestFunctionStore{}, fn, v1.Failed, io.EOF)
		require.NoError(t, err)
		require.Equal(t, v1.Failed, fn.Status.Status)
		require.Equal(t, io.EOF.Error(), fn.Status.Error)
		require.NotEmpty(t, fn.Status.LastTimeReconciled)

		// Requires truncation
		msg := strings.Repeat("a", 300)
		err = updateKQLFunctionStatus(context.Background(), &TestFunctionStore{}, fn, v1.Failed, errors.New(msg))
		require.NoError(t, err)
		require.Equal(t, v1.Failed, fn.Status.Status)
		require.Equal(t, msg[:256], fn.Status.Error)
		require.NotEmpty(t, fn.Status.LastTimeReconciled)
	})

	t.Run("update status with kusto-http error", func(t *testing.T) {
		fn := &v1.Function{
			Status: v1.FunctionStatus{
				Status: v1.Failed,
			},
		}
		body := `{"error":{"@message": "this function is invalid"}}`
		funcErr := KERRS.HTTP(KERRS.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")
		err := updateKQLFunctionStatus(context.Background(), &TestFunctionStore{}, fn, v1.Failed, funcErr)
		require.NoError(t, err)
		require.Equal(t, v1.Failed, fn.Status.Status)
		require.Equal(t, "this function is invalid", fn.Status.Error)
		require.NotEmpty(t, fn.Status.LastTimeReconciled)

		// Requires truncation
		msg := strings.Repeat("a", 300)
		body = fmt.Sprintf(`{"error":{"@message": "%s"}}`, msg)
		funcErr = KERRS.HTTP(KERRS.OpMgmt, "bad request", 400, io.NopCloser(strings.NewReader(body)), "")
		err = updateKQLFunctionStatus(context.Background(), &TestFunctionStore{}, fn, v1.Failed, funcErr)
		require.NoError(t, err)
		require.Equal(t, v1.Failed, fn.Status.Status)
		require.Equal(t, msg[:256], fn.Status.Error)
		require.NotEmpty(t, fn.Status.LastTimeReconciled)
	})
}

func newSyncFunctionsTask(t *testing.T) (*SyncFunctionsTask, *TestFunctionStore, *TestStatementExecutor, func(err error, numMgmtCalls int, numUpdates int)) {
	kustoCli := &TestStatementExecutor{database: "testdb"}
	fnStore := &TestFunctionStore{
		funcs: []*v1.Function{
			{
				ObjectMeta: metav1.ObjectMeta{
					Generation:      1,
					ResourceVersion: "sdfaasf",
					Name:            "fn1",
					Namespace:       "ns1",
				},
				Spec: v1.FunctionSpec{
					Database: "testdb",
				},
			},
		},
	}
	task := NewSyncFunctionsTask(fnStore, kustoCli)
	validate := func(err error, numMgmtCalls int, numUpdates int) {
		t.Helper()
		require.NoError(t, err)
		require.Len(t, kustoCli.stmts, numMgmtCalls)
		require.Len(t, fnStore.statusUpdates, numUpdates)
	}
	return task, fnStore, kustoCli, validate
}
