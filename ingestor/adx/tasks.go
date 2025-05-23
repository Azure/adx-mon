package adx

import (
	"context"
	ERRS "errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/cluster"
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
	Endpoint() string
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

		// If endpoints have changed, or function is not in Success, re-apply
		if t.kustoCli.Endpoint() != function.Spec.AppliedEndpoint || function.Status.Status != v1.Success || function.GetGeneration() != function.Status.ObservedGeneration {
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
			if t.kustoCli.Endpoint() != function.Spec.AppliedEndpoint {
				function.Spec.AppliedEndpoint = t.kustoCli.Endpoint()
				if err := t.store.Update(ctx, function); err != nil {
					logger.Errorf("Failed to update function %s.%s: %v", function.Spec.Database, function.Name, err)
				}
			}

			if err := t.updateKQLFunctionStatus(ctx, function, v1.Success, nil); err != nil {
				logger.Errorf("Failed to update success status: %v", err)
			}
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
		return fmt.Errorf("failed to update status for function %s.%s: %w", fn.Spec.Database, fn.Name, err)
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
			stmt = kql.New(".execute cluster script with (ThrowOnErrors = true) <|").AddUnsafe(command.Spec.Body)
		} else {
			stmt = kql.New(".execute database script with (ThrowOnErrors = true) <|").AddUnsafe(command.Spec.Body)
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

type KustoAsyncOperationState string

const (
	// KustoAsyncOperationStateCompleted indicates that the async operation has completed successfully
	KustoAsyncOperationStateCompleted KustoAsyncOperationState = "Completed"
	// KustoAsyncOperationStateFailed indicates that the async operation has failed
	KustoAsyncOperationStateFailed KustoAsyncOperationState = "Failed"
	// KustoAsyncOperationStateInProgress indicates that the async operation is in progress
	KustoAsyncOperationStateInProgress KustoAsyncOperationState = "InProgress"
	// KustoAsyncOperationStateThrottled indicates that the async operation is being throttled
	KustoAsyncOperationStateThrottled KustoAsyncOperationState = "Throttled"
	// KustoAsyncOperationStateScheduled indicates that the async operation is scheduled
	KustoAsyncOperationStateScheduled KustoAsyncOperationState = "Scheduled"
)

// IsKustoAsyncOperationStateCompleted returns true if the async operation is completed
// (either succeeded or failed)
// This is used to determine if we should poll the async operation's status.
func IsKustoAsyncOperationStateCompleted(state string) bool {
	return state == string(KustoAsyncOperationStateCompleted) ||
		state == string(KustoAsyncOperationStateFailed)
}

// Run executes the SummaryRuleTask which manages summary rules and their associated
// Kusto async operations. It handles rule submission, operation tracking, and status updates.
func (t *SummaryRuleTask) Run(ctx context.Context) error {
	// Fetch all summary rules from storage
	summaryRules := &v1.SummaryRuleList{}
	if err := t.store.List(ctx, summaryRules); err != nil {
		return fmt.Errorf("failed to list summary rules: %w", err)
	}

	// Get the status of all async operations currently tracked in Kusto
	// to match against our rules' operations
	kustoAsyncOperations, err := t.GetOperations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get async operations: %w", err)
	}

	// Process each summary rule individually
	for _, rule := range summaryRules.Items {
		// Skip rules not belonging to the current database
		if rule.Spec.Database != t.kustoCli.Database() {
			continue
		}

		// Get the current condition of the rule
		cnd := rule.GetCondition()
		if cnd == nil {
			// For first-time execution, initialize the condition with a timestamp
			// that's one interval back from current time
			cnd = &metav1.Condition{
				LastTransitionTime: metav1.Time{Time: time.Now().Add(-rule.Spec.Interval.Duration)},
			}
		}

		// Determine if the rule should be executed based on several criteria:
		// 1. The rule is being deleted
		// 2. Previous submission failed
		// 3. Rule has been updated (new generation)
		// 4. It's time for the next interval execution
		shouldSubmitRule := rule.DeletionTimestamp != nil || // Rule is being deleted
			cnd.Status == metav1.ConditionFalse || // Submission failed, so no async operation was created for this interval
			cnd.ObservedGeneration != rule.GetGeneration() || // A new version of this CRD was created
			time.Since(cnd.LastTransitionTime.Time) >= rule.Spec.Interval.Duration // It's time to execute for a new interval

		if shouldSubmitRule {
			// Prepare a new async operation with time range from last execution to now
			asyncOp := v1.AsyncOperation{
				StartTime: cnd.LastTransitionTime.UTC().Truncate(time.Minute).Format(time.RFC3339Nano),
				EndTime:   time.Now().UTC().Truncate(time.Minute).Format(time.RFC3339Nano),
			}
			operationId, err := t.SubmitRule(ctx, rule, asyncOp.StartTime, asyncOp.EndTime)
			if err != nil {
				t.store.UpdateStatus(ctx, &rule, err)
			} else {

				asyncOp.OperationId = operationId
				rule.SetAsyncOperation(asyncOp)
			}
		}

		// Process any outstanding async operations for this rule
		operations := rule.GetAsyncOperations()
		foundOperations := make(map[string]bool)

		for _, op := range operations {
			found := false
			for _, kustoOp := range kustoAsyncOperations {
				if op.OperationId == kustoOp.OperationId {
					found = true
					foundOperations[op.OperationId] = true

					if IsKustoAsyncOperationStateCompleted(kustoOp.State) {
						// If the operation completed successfully, verify the table exists before marking as complete
						if kustoOp.State == string(KustoAsyncOperationStateCompleted) {
							// Ensure the summary table exists
							if err := t.ensureTableExists(ctx, rule.Spec.Table); err != nil {
								logger.Errorf("Failed to ensure table %s exists: %v", rule.Spec.Table, err)
								// Store the error in the operation status but don't remove the operation yet
								// This will prevent the rule from being marked as complete until the table exists
								continue
							}
							// Table exists, so we can safely mark the operation as complete
							logger.Infof("Table %s verified to exist, marking operation %s as complete", rule.Spec.Table, kustoOp.OperationId)
						}
						// We're done polling this async operation, so we can remove it from the list
						rule.RemoveAsyncOperation(kustoOp.OperationId)
					}
					if kustoOp.ShouldRetry != 0 {
						if _, err := t.SubmitRule(ctx, rule, op.StartTime, op.EndTime); err != nil {
							logger.Errorf("Failed to submit rule: %v", err)
						}
					}

					// Operation is still in progress, so we can skip it
				}
				// If the operation wasn't found in the Kusto operations list,
				// it has fallen out of the backlog window
				if !found {
					logger.Infof("Async operation %s for rule %s has fallen out of the Kusto backlog window, removing",
						op.OperationId, rule.Name)
					rule.RemoveAsyncOperation(op.OperationId)
				}
			}
		}

		if err := t.store.UpdateStatus(ctx, &rule, nil); err != nil {
			logger.Errorf("Failed to update summary rule status: %v", err)
			// Not a lot we can do here, we'll end up just retrying next interval.
		}
	}

	return nil
}

func (t *SummaryRuleTask) SubmitRule(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
	// NOTE: We cannot do something like `let _startTime = datetime();` as dot-command do not permit
	// preceding let-statements.
	rule.Spec.Body = strings.ReplaceAll(rule.Spec.Body, "_startTime", fmt.Sprintf("datetime(%s)", startTime))
	rule.Spec.Body = strings.ReplaceAll(rule.Spec.Body, "_endTime", fmt.Sprintf("datetime(%s)", endTime))
	// Execute asynchronously
	stmt := kql.New(".set-or-append async ").AddUnsafe(rule.Spec.Table).AddLiteral(" <| ").AddUnsafe(rule.Spec.Body)
	res, err := t.kustoCli.Mgmt(ctx, stmt)
	if err != nil {
		return "", fmt.Errorf("failed to execute summary rule %s.%s: %w", rule.Spec.Database, rule.Name, err)
	}

	return operationIDFromResult(res)
}

// ensureTableExists checks if the table specified in the rule exists, and if not, creates it.
// This is necessary because sometimes Kusto async operations mark as complete but don't create the table.
func (t *SummaryRuleTask) ensureTableExists(ctx context.Context, tableName string) error {
	// Check if table exists
	stmt := kql.New(".show tables | where TableName == '").AddUnsafe(tableName).AddLiteral("'")
	rows, err := t.kustoCli.Mgmt(ctx, stmt)
	if err != nil {
		return fmt.Errorf("failed to check if table %s exists: %w", tableName, err)
	}
	
	// Check if there are any rows in the result (table exists)
	tableExists := false
	if rows != nil {
		defer rows.Stop()
		for {
			_, errInline, errFinal := rows.NextRowOrError()
			if errFinal == io.EOF {
				break
			}
			if errInline != nil {
				continue
			}
			if errFinal != nil {
				return fmt.Errorf("error checking if table %s exists: %w", tableName, errFinal)
			}
			
			tableExists = true
			break
		}
	}
	
	if !tableExists {
		logger.Infof("Table %s does not exist, creating it", tableName)
		// Create the table with a minimal schema to ensure it exists
		createStmt := kql.New(".create-merge table ['").AddUnsafe(tableName).AddLiteral("'] (Timestamp: datetime)")
		_, err := t.kustoCli.Mgmt(ctx, createStmt)
		if err != nil {
			return fmt.Errorf("failed to create table %s: %w", tableName, err)
		}
		logger.Infof("Successfully created table %s", tableName)
	}
	
	return nil
}

func (t *SummaryRuleTask) GetOperations(ctx context.Context) ([]AsyncOperationStatus, error) {
	// List all the async operations that have been executed in the last 24 hours. If one of our
	// async operations falls out of this window, it's time to stop trying that particular operation.
	stmt := kql.New(".show operations | where StartedOn > ago(1d) | where Operation == 'TableSetOrAppend' | summarize arg_max(LastUpdatedOn, OperationId, State, ShouldRetry) by OperationId | project LastUpdatedOn, OperationId = tostring(OperationId), State, ShouldRetry = todouble(ShouldRetry) | sort by LastUpdatedOn asc")
	rows, err := t.kustoCli.Mgmt(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve async operations: %w", err)
	}
	defer rows.Stop()

	var operations []AsyncOperationStatus
	for {
		row, errInline, errFinal := rows.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return nil, fmt.Errorf("failed to retrieve async operations: %v", errFinal)
		}

		var status AsyncOperationStatus
		if err := row.ToStruct(&status); err != nil {
			return nil, fmt.Errorf("failed to parse async operation: %v", err)
		}
		if status.State == "" {
			continue
		}

		operations = append(operations, status)
	}

	return operations, nil
}

func operationIDFromResult(iter *kusto.RowIterator) (string, error) {
	defer iter.Stop()

	for {
		row, errInline, errFinal := iter.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return "", fmt.Errorf("failed to retrieve operation ID: %v", errFinal)
		}

		if len(row.Values) != 1 {
			return "", fmt.Errorf("unexpected number of values in row: %d", len(row.Values))
		}

		return row.Values[0].String(), nil
	}

	return "", nil
}

type AsyncOperationStatus struct {
	OperationId   string    `kusto:"OperationId"`
	LastUpdatedOn time.Time `kusto:"LastUpdatedOn"`
	State         string    `kusto:"State"`
	ShouldRetry   float64   `kusto:"ShouldRetry"`
}

type AuditDiskSpaceTask struct {
	QueueSizer cluster.QueueSizer
	StorageDir string
}

func NewAuditDiskSpaceTask(queueSizer cluster.QueueSizer, storageDir string) *AuditDiskSpaceTask {
	return &AuditDiskSpaceTask{
		QueueSizer: queueSizer,
		StorageDir: storageDir,
	}
}

func (t *AuditDiskSpaceTask) Run(ctx context.Context) error {
	var actualSize int64
	var actualCount int64

	err := filepath.Walk(t.StorageDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".wal") {
			actualSize += info.Size()
			actualCount++
		}
		return nil
	})
	if err != nil {
		logger.Errorf("AuditDiskSpaceTask: failed to walk storage dir: %v", err)
		return err
	}

	expectedSize := t.QueueSizer.SegmentsSize()
	expectedCount := t.QueueSizer.SegmentsTotal()

	const oneGB = float64(1 << 30)
	if math.Abs(float64(actualSize-expectedSize)) > oneGB || math.Abs(float64(actualCount-expectedCount)) > 10 {
		logger.Warnf("AuditDiskSpaceTask: WAL segment disk usage mismatch: size actual=%d expected=%d, segments actual=%d expected=%d", actualSize, expectedSize, actualCount, expectedCount)
	}
	return nil
}
