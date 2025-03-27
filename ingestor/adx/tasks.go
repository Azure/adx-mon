package adx

import (
	"context"
	ERRS "errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/errors"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TableDetail struct {
	TableName       string  `kusto:"TableName"`
	HotExtentSize   float64 `kusto:"HotExtentSize"`
	TotalExtentSize float64 `kusto:"TotalExtentSize"`
	TotalExtents    int64   `kusto:"TotalExtents"`
	HotRowCount     int64   `kusto:"HotRowCount"`
	TotalRowCount   int64   `kusto:"TotalRowCount"`
}

type DropUnusedTablesTask struct {
	mu           sync.Mutex
	unusedTables map[string]int
	kustoCli     StatementExecutor
	database     string
}

type StatementExecutor interface {
	Database() string
	Mgmt(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error)
}

func NewDropUnusedTablesTask(kustoCli StatementExecutor) *DropUnusedTablesTask {
	return &DropUnusedTablesTask{
		unusedTables: make(map[string]int),
		kustoCli:     kustoCli,
	}
}

func (t *DropUnusedTablesTask) Run(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	details, err := t.loadTableDetails(ctx)
	if err != nil {
		return fmt.Errorf("error loading table details: %w", err)
	}

	for _, v := range details {
		if v.TotalRowCount > 0 {
			delete(t.unusedTables, v.TableName)
		}

		if v.TotalRowCount == 0 {
			t.unusedTables[v.TableName]++
			logger.Infof("Marking table %s.%s as unused", t.database, v.TableName)
		}
	}

	for table, count := range t.unusedTables {
		if count > 2 {
			logger.Infof("DRYRUN Dropping unused table %s.%s", t.kustoCli.Database(), table)
			// stmt := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(fmt.Sprintf(".drop table %s", table))
			// if _, err := t.kustoCli.Mgmt(ctx, stmt); err != nil {
			// 	return fmt.Errorf("error dropping table %s: %w", table, err)
			// }
			delete(t.unusedTables, table)
		}
	}
	return nil
}

func (t *DropUnusedTablesTask) loadTableDetails(ctx context.Context) ([]TableDetail, error) {
	stmt := kusto.NewStmt(".show tables details | project TableName, HotExtentSize, TotalExtentSize, TotalExtents, HotRowCount, TotalRowCount")
	rows, err := t.kustoCli.Mgmt(ctx, stmt)
	if err != nil {
		return nil, err
	}

	var tables []TableDetail
	for {
		row, err1, err2 := rows.NextRowOrError()
		if err2 == io.EOF {
			return tables, nil
		} else if err1 != nil {
			return tables, err1
		} else if err2 != nil {
			return tables, err2
		}

		var v TableDetail
		if err := row.ToStruct(&v); err != nil {
			return tables, err
		}
		tables = append(tables, v)
	}
}

type SyncFunctionsTask struct {
	store    storage.Functions
	kustoCli StatementExecutor
}

func NewSyncFunctionsTask(store storage.Functions, kustoCli StatementExecutor) *SyncFunctionsTask {
	return &SyncFunctionsTask{
		store:    store,
		kustoCli: kustoCli,
	}
}

func (t *SyncFunctionsTask) Run(ctx context.Context) error {
	functions, err := t.store.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list functions: %w", err)
	}
	for _, function := range functions {

		if function.Spec.Database != v1.AllDatabases && function.Spec.Database != t.kustoCli.Database() {
			continue
		}

		if !function.DeletionTimestamp.IsZero() {
			// Until we can parse KQL we don't actually know the function's
			// name as described in this CRD; however, we'll make the assumption
			// that the CRD name is the same as the function name in Kusto and
			// attempt a delete.
			stmt := kql.New(".drop function ").AddUnsafe(function.Name).AddLiteral(" ifexists")
			if _, err := t.kustoCli.Mgmt(ctx, stmt); err != nil {
				logger.Errorf("Failed to delete function %s.%s: %v", function.Spec.Database, function.Name, err)
				// Deletion is best-effort, especially while we still can't parse KQL
			}
			t.updateKQLFunctionStatus(ctx, function, v1.Success, nil)
			return nil
		}

		stmt := kql.New(".execute database script with (ThrowOnErrors=true) <| ").AddUnsafe(function.Spec.Body)
		if _, err := t.kustoCli.Mgmt(ctx, stmt); err != nil {
			if !errors.Retry(err) {
				logger.Errorf("Permanent failure to create function %s.%s: %v", function.Spec.Database, function.Name, err)
				if err = t.updateKQLFunctionStatus(ctx, function, v1.PermanentFailure, err); err != nil {
					logger.Errorf("Failed to update permanent failure status: %v", err)
				}
				continue
			} else {
				t.updateKQLFunctionStatus(ctx, function, v1.Failed, err)
				logger.Warnf("Transient failure to create function %s.%s: %v", function.Spec.Database, function.Name, err)
				continue
			}
		}

		logger.Infof("Successfully created function %s.%s", function.Spec.Database, function.Name)
		if err := t.updateKQLFunctionStatus(ctx, function, v1.Success, nil); err != nil {
			logger.Errorf("Failed to update success status: %v", err)
		}
	}

	return nil
}

func (t *SyncFunctionsTask) updateKQLFunctionStatus(ctx context.Context, fn *v1.Function, status v1.FunctionStatusEnum, err error) error {
	fn.Status.Status = status
	if err != nil {
		errMsg := err.Error()

		var kustoerr *errors.HttpError
		if ERRS.As(err, &kustoerr) {
			decoded := kustoerr.UnmarshalREST()
			if errMap, ok := decoded["error"].(map[string]interface{}); ok {
				if errMsgVal, ok := errMap["@message"].(string); ok {
					errMsg = errMsgVal
				}
			}
		}

		if len(errMsg) > 256 {
			errMsg = errMsg[:256]
		}
		fn.Status.Error = errMsg
	}
	if err := t.store.UpdateStatus(ctx, fn); err != nil {
		return fmt.Errorf("Failed to update status for function %s.%s: %w", fn.Spec.Database, fn.Name, err)
	}
	return nil
}

type ManagementCommandTask struct {
	store    storage.CRDHandler
	kustoCli StatementExecutor
}

func NewManagementCommandsTask(store storage.CRDHandler, kustoCli StatementExecutor) *ManagementCommandTask {
	return &ManagementCommandTask{
		store:    store,
		kustoCli: kustoCli,
	}
}

func (t *ManagementCommandTask) Run(ctx context.Context) error {
	managementCommands := &v1.ManagementCommandList{}
	if err := t.store.List(ctx, managementCommands, storage.FilterCompleted); err != nil {
		return fmt.Errorf("failed to list management commands: %w", err)
	}
	for _, command := range managementCommands.Items {
		// ManagementCommands database is optional as not all commands are scoped at the database level
		if command.Spec.Database != "" && command.Spec.Database != t.kustoCli.Database() {
			continue
		}

		var stmt *kql.Builder
		if command.Spec.Database == "" {
			stmt = kql.New(".execute cluster script <|").AddUnsafe(command.Spec.Body)
		} else {
			stmt = kql.New(".execute database script <|").AddUnsafe(command.Spec.Body)
		}
		if _, err := t.kustoCli.Mgmt(ctx, stmt); err != nil {
			logger.Errorf("Failed to execute management command %s.%s: %v", command.Spec.Database, command.Name, err)
			if err = t.store.UpdateStatus(ctx, &command, err); err != nil {
				logger.Errorf("Failed to update management command status: %v", err)
			}
		}

		logger.Infof("Successfully executed management command %s.%s", command.Spec.Database, command.Name)
		if err := t.store.UpdateStatus(ctx, &command, nil); err != nil {
			logger.Errorf("Failed to update success status: %v", err)
		}
	}

	return nil
}

type SummaryRuleTask struct {
	store    storage.CRDHandler
	kustoCli StatementExecutor
}

func NewSummaryRuleTask(store storage.CRDHandler, kustoCli StatementExecutor) *SummaryRuleTask {
	return &SummaryRuleTask{
		store:    store,
		kustoCli: kustoCli,
	}
}
func (t *SummaryRuleTask) Run(ctx context.Context) error {
	summaryRules := &v1.SummaryRuleList{}
	if err := t.store.List(ctx, summaryRules); err != nil {
		return fmt.Errorf("failed to list summary rules: %w", err)
	}
	for _, rule := range summaryRules.Items {
		if rule.Spec.Database != t.kustoCli.Database() {
			continue
		}
		if rule.DeletionTimestamp != nil {
			// rule is being deleted, we might possibly want to delete the associated
			// destination table, but there is a potential for unintended historical
			// data loss if the user doesn't understand this consequence.
			continue
		}

		cnd := rule.GetCondition()
		if cnd != nil && cnd.Status == metav1.ConditionTrue && time.Since(cnd.LastTransitionTime.Time) < rule.Spec.Interval.Duration {
			if logger.IsDebug() {
				logger.Debugf("Skipping summary rule %s.%s as it was executed recently %s", rule.Spec.Database, rule.Name, cnd.LastTransitionTime)
			}
			continue
		}

		if cnd == nil {
			cnd = &metav1.Condition{}
		}

		// Execution occurs asynchronously, so we don't have to concern ourselves with blocking
		startTime := time.Now().Add(-rule.Spec.Interval.Duration).Truncate(time.Minute)
		endTime := time.Now().Truncate(time.Minute)
		if !cnd.LastTransitionTime.IsZero() {
			startTime = cnd.LastTransitionTime.Time
		}

		// Save our endTime, which will become the next iteration's startTime
		cnd.LastTransitionTime = metav1.Time{Time: endTime}
		rule.SetCondition(*cnd)

		// NOTE: We cannot do something like `let _startTime = datetime();` as dot-command do not permit
		// preceding let-statements.
		rule.Spec.Body = strings.ReplaceAll(rule.Spec.Body, "_startTime", fmt.Sprintf("datetime(%s)", startTime.UTC().Format(time.RFC3339Nano)))
		rule.Spec.Body = strings.ReplaceAll(rule.Spec.Body, "_endTime", fmt.Sprintf("datetime(%s)", endTime.UTC().Format(time.RFC3339Nano)))
		// Execute asynchronously
		stmt := kql.New(".set-or-append async ").AddUnsafe(rule.Spec.Table).AddLiteral(" <| ").AddUnsafe(rule.Spec.Body)
		_, err := t.kustoCli.Mgmt(ctx, stmt)
		if err != nil {
			logger.Errorf("Failed to execute summary rule %s.%s: %v", rule.Spec.Database, rule.Name, err)
			continue
		}
		if err = t.store.UpdateStatus(ctx, &rule, err); err != nil {
			logger.Errorf("Failed to update summary rule status: %v", err)
		}
	}
	return nil
}
