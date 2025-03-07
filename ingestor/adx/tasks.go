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

		if function.Spec.Database != t.kustoCli.Database() {
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

		stmt := kql.New(".execute script<|").AddUnsafe(command.Spec.Body)
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

		cnd := rule.GetCondition()
		if !ShouldSummaryRule(cnd, rule) {
			// If we do not need to execute the SummaryRule as determined
			// by the predicate, we know the SummaryRule has successfully
			// been submitted to Kusto and is up-to-date as determined
			// by the CRD's execution interval.
			if logger.IsDebug() {
				logger.Debugf("Skipping summary rule %s.%s as it was executed recently %s", rule.Spec.Database, rule.Name, cnd.LastTransitionTime)
			}

			// At this point the SummaryRule has successfully been submitted to
			// Kuto, now we check if it's necessary to poll the async operation.
			if ShouldSummaryRuleAsync(rule) {
				if err := t.pollAsyncOperation(ctx, rule); err != nil {
					logger.Errorf("Failed to poll async operation for summary rule %s.%s: %v", rule.Spec.Database, rule.Name, err)
				}
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
		res, err := t.kustoCli.Mgmt(ctx, stmt)
		if err != nil {
			logger.Errorf("Failed to execute summary rule %s.%s: %v", rule.Spec.Database, rule.Name, err)
			continue
		}

		// We store the async operation's ID as a subcondition and periodically poll its
		// status before setting the async operation's final result. The subcondition is
		// stored on the CRD when UpdateStatus is invoked.
		operationId, err := operationIDFromResult(res)
		if err != nil {
			logger.Errorf("Failed to retrieve operation ID for summary rule %s.%s: %v", rule.Spec.Database, rule.Name, err)
			continue
		}
		opcnd := metav1.Condition{
			Status:  metav1.ConditionUnknown,
			Reason:  "Created",
			Message: operationId,
		}
		rule.SetOperationIDCondition(opcnd)

		// Persist the task's status, both for the async operation and the immediate
		// acceptance of the management command.
		if err = t.store.UpdateStatus(ctx, &rule, err); err != nil {
			logger.Errorf("Failed to update summary rule status: %v", err)
		}
	}
	return nil
}

func (t *SummaryRuleTask) pollAsyncOperation(ctx context.Context, rule v1.SummaryRule) error {
	cnd := rule.GetOperationIDCondition()
	if cnd == nil {
		// We shouldn't ever hit this unless the ShouldSummaryRuleAsync predicate is incorrect.
		return fmt.Errorf("no operation ID condition found for summary rule %s.%s", rule.Spec.Database, rule.Name)
	}

	stmt := kql.New(".show operations").
		AddLiteral(" | where StartedOn > ").AddUnsafe(fmt.Sprintf("datetime(%s)", cnd.LastTransitionTime.UTC().Format(time.RFC3339Nano))).
		AddLiteral(" | where OperationId == ").AddString(cnd.Message).
		AddLiteral(" | summarize arg_max(LastUpdatedOn, State)")
	rows, err := t.kustoCli.Mgmt(ctx, stmt)
	if err != nil {
		return fmt.Errorf("failed to poll async operation %s: %w", cnd.Message, err)
	}
	defer rows.Stop()

	for {
		row, errInline, errFinal := rows.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return fmt.Errorf("failed to retrieve async operation %s: %v", cnd.Message, errFinal)
		}

		var status AsyncOperationStatus
		if err := row.ToStruct(&status); err != nil {
			return fmt.Errorf("failed to parse async operation %s: %v", cnd.Message, err)
		}
		if status.State == "" {
			continue
		}

		status.ShouldRetry, err = t.shouldRetryTask(ctx, cnd.Message, status.State)
		if err != nil {
			return fmt.Errorf("failed to check async operation %s: %w", cnd.Message, err)
		}

		// We're only interested in the most recent operation status.
		// Possible values for State are:
		// https://learn.microsoft.com/en-us/kusto/management/show-operations
		switch status.State {
		case "Completed":
			// Operation succeeded
			cnd.Status = metav1.ConditionTrue
			cnd.Reason = "Succeeded"
		case "Failed":
			// If the operation failed, we need to check if we should retry
			if status.ShouldRetry {
				// Kuso thinks it's worth retrying. We'll set the management submission
				// condition to False, which will have the effect of re-submitting the command.
				cmdCnd := rule.GetCondition()
				if cmdCnd != nil {
					cmdCnd.Status = metav1.ConditionFalse
					cmdCnd.Reason = "Failed"
					cmdCnd.Message = "Async operation failed, but should be retried"
					cmdCnd.LastTransitionTime = metav1.Time{Time: time.Now()}
					rule.SetCondition(*cmdCnd)
				}

				// We're going to resubmit this task, so we can keep the async
				// operation's status as Unknown, though we'll update the Reason.
				cnd.Reason = "Retry"
			} else {
				// This is a terminal failure, so we'll update the condition accordingly.
				cnd.Status = metav1.ConditionFalse
				cnd.Reason = "Failed"
			}

		case "Scheduled", "InProgress":
			// Just record what's happening and we'll check again later.
			cnd.Reason = status.State

		case "Throttled":
			// NOTE:
			// We're being throttled, so this async operation will not be retried
			// and we shouldn't retry either. We need to log this and set the status.
			// This should be an actionable state. Triage should occur to determine
			// if we're doing something too aggressively or if we need to scale out
			// to accommodate the load.
			logger.Errorf("Kusto operations are being throttled: %s", t.kustoCli.Database())
			cnd.Reason = status.State
			cnd.Status = metav1.ConditionFalse

		default:
			// All other reasons are non-actionable. We simply record what has occured
			// and move on. In all cases, the async operation is considered to be failed
			// since it's in a non-actionable non-succeeding state.
			cnd.Reason = status.State
			cnd.Status = metav1.ConditionFalse
		}

		rule.SetOperationIDCondition(*cnd)
		if err = t.store.UpdateStatus(ctx, &rule, err); err != nil {
			return fmt.Errorf("failed to update summary rule status: %w", err)
		}

		return nil
	}

	return nil
}

func (t *SummaryRuleTask) shouldRetryTask(ctx context.Context, operationId, state string) (bool, error) {
	// For reasons I don't understand, the GO SDK is not able to execute the query
	// `.show operations | where OperationId == "operationId" | summarize arg_max(LastUpdatedOn, State, ShouldRetry)`.
	// The client library itself throws an error stating that ShouldRetry should be a bool but is a float64.
	// Even if I cast ShouldRetry to a bool, the error persists.
	//
	// This method is a work-around where we query if the operation should be retried.
	// `.show operations | where OperationId == "operationId" | where State == "state" | where ShouldRetry == true | count`
	// If the value is non zero, we should retry.
	stmt := kql.New(".show operations").
		AddLiteral(" | where OperationId == ").AddString(operationId).
		AddLiteral(" | where State == ").AddString(state).
		AddLiteral(" | where ShouldRetry == true | count")
	rows, err := t.kustoCli.Mgmt(ctx, stmt)
	if err != nil {
		return false, fmt.Errorf("failed to check async operation retry %s: %w", operationId, err)
	}
	defer rows.Stop()

	for {
		row, errInline, errFinal := rows.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return false, fmt.Errorf("failed to check if operation should be retried: %v", errFinal)
		}

		// The query returns a single row with a single "Count" column
		if len(row.Values) != 1 {
			return false, fmt.Errorf("unexpected number of values in row: %d", len(row.Values))
		}
		return row.Values[0].String() != "0", nil
	}

	return false, nil
}

// ShouldSummaryRule returns true if the SummaryRule should be executed
func ShouldSummaryRule(cnd *metav1.Condition, rule v1.SummaryRule) bool {
	// If a status condition isn't yet set, it means this SummaryRule
	// hasn't yet executed.
	if cnd == nil {
		return true
	}
	// If the rule is being deleted, we don't need to execute it.
	if rule.DeletionTimestamp != nil {
		return false
	}
	// We know the rule has been updated and therfore needs
	// execution if our observed generation is different from the
	// SummaryRule's generation.
	if rule.GetGeneration() != cnd.ObservedGeneration {
		return true
	}
	// If the SummaryRule is in a failed state.
	if cnd.Status == metav1.ConditionFalse {
		return true
	}
	// If the SummaryRule is in a succeeded state, we need to check
	// if the interval has passed since the last execution.
	if time.Since(cnd.LastTransitionTime.Time) > rule.Spec.Interval.Duration {
		return true
	}
	return false
}

// ShouldSummaryRuleAsync returns true if the SummaryRule's async operation
// is still pending. This is used to determine if we should poll the
// operation's status.
func ShouldSummaryRuleAsync(rule v1.SummaryRule) bool {
	cnd := rule.GetOperationIDCondition()
	// If no condition exists, there's nothing to check.
	if cnd == nil {
		return false
	}
	// If the rule is being deleted, there's no need to poll the async operation.
	if rule.DeletionTimestamp != nil {
		return false
	}
	// If there is a new rule generation, it doesn't make sense to
	// poll for the previous rule's async operation as its meaning
	// is ambiguous.
	if rule.GetGeneration() != cnd.ObservedGeneration {
		return false
	}
	// If the async operation has completed and we've stored
	// that state, pass or fail, there's nothing for us to do.
	if cnd.Status == metav1.ConditionFalse || cnd.Status == metav1.ConditionTrue {
		return false
	}
	// If the async operation is still pending, we need to
	// poll its status. In addition, we need to check if the
	// async operation has already recently been checked and
	// if so wait for some time before checking again.
	if cnd.Status == metav1.ConditionUnknown && time.Since(cnd.LastTransitionTime.Time) > v1.SummaryRuleAsyncOperationPollInterval {
		return true
	}
	return false
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
	LastUpdatedOn time.Time `kusto:"LastUpdatedOn"`
	State         string    `kusto:"State"`
	ShouldRetry   bool      `kusto:"ShouldRetry"`
}
