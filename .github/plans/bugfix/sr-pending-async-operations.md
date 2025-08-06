# Bugfix Plan: Replace Bulk Operations Query with Individual Operation Lookups

## Summary
Operations in SummaryRule status become permanently stuck when they fall outside the 24-hour Kusto lookback window used by `getOperations()`. This causes SummaryRules to remain in "InProgress" state indefinitely. The solution is to replace the bulk operations query with individual operation lookups that can find operations regardless of age.

## ⚠️ CRITICAL IMPLEMENTATION GUIDANCE ⚠️

**This is an extremely focused and surgical change. The implementing agent MUST:**

1. **Only make changes explicitly outlined in this plan** - Do not add additional features or improvements
2. **Verify each change is necessary** - Before making any modification, confirm it's required for the fix
3. **Preserve all existing functionality** - Especially operations with empty `OperationId` (backlog operations)
4. **Follow the exact implementation pattern** provided in the code examples
5. **Use the checklist below** to stay on track and verify completion

## Implementation Checklist

### Pre-Implementation Verification
- [x] Read and understand the entire plan before starting
- [x] Confirm understanding of the bug: operations >24h old get stuck due to bulk query limitation
- [x] Verify that the fix approach (individual lookups) addresses the root cause
- [x] Understand that empty `OperationId` operations must remain completely unaffected

### Task 1 Checklist - Add getOperation Method
- [x] Add exactly one new method: `getOperation(ctx context.Context, operationId string) (*AsyncOperationStatus, error)`
- [x] Use the exact KQL query pattern provided in the plan (with parameterized queries)
- [x] Return `*AsyncOperationStatus` for found operations, `nil` for not found, `error` for failures
- [x] Add defensive error handling and parsing
- [x] Verify method signature matches plan exactly
- [x] Test the new method works with mock data
- [x] Implement proper MockRows-based testing for complete coverage
- [x] Test all scenarios: operation found, not found, query errors, and data parsing

### Task 2 Checklist - Replace Bulk Query Usage
- [x] Modify `trackAsyncOperations()` method only
- [x] Replace the bulk operation lookup logic with individual `getOperation()` calls
- [x] **CRITICAL**: Verify operations with empty `OperationId` still go to `processBacklogOperation()`
- [x] Preserve existing retry logic: `handleRetryOperation()` calls remain unchanged
- [x] Preserve existing completion logic: `handleCompletedOperation()` calls remain unchanged  
- [x] Add error handling for individual query failures (log and continue)
- [x] Verify no other method signatures need to change

### Task 3 Checklist - Remove Bulk Query
- [ ] Remove `getOperations()` method from `SummaryRuleTask`
- [ ] Update `initializeRun()` to not call `getOperations()`
- [ ] Remove `kustoAsyncOperations` parameter from `trackAsyncOperations()`
- [ ] Update method signature in `SummaryRuleTask` struct if needed
- [ ] Verify no other code references the removed method

### Task 4 Checklist - Tests and Documentation  
- [ ] Add unit tests for `getOperation()` method only
- [ ] Add tests for `trackAsyncOperations()` with individual lookups
- [ ] **CRITICAL**: Test that empty `OperationId` operations are unaffected
- [ ] Update existing tests that mock `getOperations()` to mock individual calls
- [ ] Add code comments explaining the change from bulk to individual queries

### Post-Implementation Verification
- [ ] All existing tests pass without modification (except mocking changes)
- [ ] Operations with `OperationId` can be found regardless of age
- [ ] Operations with empty `OperationId` still processed by `processBacklogOperation()`
- [ ] No performance degradation for typical operation counts (<10 per rule)
- [ ] Error handling works correctly for query failures

## Sample Broken SummaryRule

```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: example-summary-rule
  namespace: monitoring
spec:
  database: TestDB
  table: SummaryData
  interval: 1h
  body: |
    let data = SourceTable
    | where TimeStamp between(_startTime .. _endTime)
    | summarize count() by bin(TimeStamp, 1h)
status:
  conditions:
  - type: summaryrule.adx-mon.azure.com/OperationId
    status: Unknown
    reason: InProgress
    message: '[{"operationId":"a1b2c3d4-e5f6-7890-1234-567890abcdef","startTime":"2025-08-01T10:00:00Z","endTime":"2025-08-01T10:59:59.9999999Z"},{"operationId":"b2c3d4e5-f6g7-8901-2345-67890abcdef1","startTime":"2025-08-01T11:00:00Z","endTime":"2025-08-01T11:59:59.9999999Z"}]'
  - type: summaryrule.adx-mon.azure.com/LastSuccessfulExecution
    status: "True"
    reason: ExecutionCompleted
    message: "2025-08-01T12:00:00Z"
```

In this example, if the current date is more than 24 hours after August 1, 2025, the operations `a1b2c3d4-e5f6-7890-1234-567890abcdef` and `b2c3d4e5-f6g7-8901-2345-67890abcdef1` will never be found by `getOperations()` because they fall outside Kusto's 24-hour operations history lookback window.

## Analysis of the Problem

### Current System Flow
1. `SummaryRuleTask.Run()` calls `getOperations()` to fetch **all operations from last 24 hours only**
2. `trackAsyncOperations()` iterates through operations stored in the SummaryRule status
3. For operations with `OperationId`, it searches for them in the 24h operations list
4. **If operation is not found** (outside 24h window), it continues without action
5. Operation remains permanently in status, causing rule to stay "InProgress"

### Root Cause
The `getOperations()` method uses a hardcoded 24-hour lookback window:
```kusto
.show operations | where StartedOn > ago(1d) | ...
```

Operations that were submitted more than 24 hours ago cannot be found, even though they may still exist in Kusto's operations history and can be queried individually.

### Issue Impact
- SummaryRules stuck in "InProgress" state indefinitely
- No resolution for operations outside the 24-hour window
- Accumulation of unreachable operations in status messages

## Identification of the Bug

**Location**: `ingestor/adx/tasks.go:527-560` in `getOperations()` and `ingestor/adx/tasks.go:446-452` in `trackAsyncOperations()`

**Bug Code**:
```go
// getOperations() - hardcoded 24h limitation
stmt := kql.New(".show operations | where StartedOn > ago(1d) | ...")

// trackAsyncOperations() - searches only in 24h results
index := slices.IndexFunc(kustoAsyncOperations, func(item AsyncOperationStatus) bool {
    return item.OperationId == op.OperationId
})

if index == -1 {
    continue  // ❌ BUG: Operations outside 24h window are ignored forever
}
```

**Expected Behavior**: Operations with `OperationId` should be queryable directly regardless of age.

**Actual Behavior**: Operations are ignored indefinitely when they age out of the 24-hour bulk query window.

## Recommended Fix

### Strategy
Replace the bulk `getOperations()` method with individual `getOperation()` lookups that can query for specific operations by ID without time-based limitations. This eliminates the 24-hour lookback window issue entirely while preserving all existing functionality.

### Implementation Approach
1. **Add `getOperation()` method**: Query for specific operations by OperationId using parameterized queries
2. **Modify `trackAsyncOperations()`**: Replace bulk operation lookup with individual queries
3. **Preserve backlog operations**: Ensure operations with empty `OperationId` continue normal processing
4. **Maintain existing logic**: Preserve retry and completion handling for found operations

### Key Benefits
- **Eliminates 24-hour limitation**: Can find operations regardless of age
- **Better performance**: Only queries for operations we actually need to track
- **Cleaner logic**: Direct lookup instead of searching filtered bulk results
- **Preserves all functionality**: No changes to backlog operation handling

## Sample Broken SummaryRule
```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: example-summary-rule
  namespace: monitoring
spec:
  database: TestDB
  table: SummaryData
  interval: 1h
  body: |
    let data = SourceTable
    | where TimeStamp between(_startTime .. _endTime)
    | summarize count() by bin(TimeStamp, 1h)
status:
  conditions:
  - type: summaryrule.adx-mon.azure.com/OperationId
    status: Unknown
    reason: InProgress
    message: '[{"operationId":"a1b2c3d4-e5f6-7890-1234-567890abcdef","startTime":"2025-08-01T10:00:00Z","endTime":"2025-08-01T10:59:59.9999999Z"},{"operationId":"b2c3d4e5-f6g7-8901-2345-67890abcdef1","startTime":"2025-08-01T11:00:00Z","endTime":"2025-08-01T11:59:59.9999999Z"}]'
  - type: summaryrule.adx-mon.azure.com/LastSuccessfulExecution
    status: "True"
    reason: ExecutionCompleted
    message: "2025-08-01T12:00:00Z"
```

In this example, if the current date is more than 24 hours after August 1, 2025, the operations `a1b2c3d4-e5f6-7890-1234-567890abcdef` and `b2c3d4e5-f6g7-8901-2345-67890abcdef1` will never be found by `getOperations()` because they fall outside Kusto's 24-hour operations history lookback window.

## Analysis of the Problem

### Current System Flow
1. `SummaryRuleTask.Run()` calls `getOperations()` to fetch **all operations from last 24 hours only**
2. `trackAsyncOperations()` iterates through operations stored in the SummaryRule status
3. For operations with `OperationId`, it searches for them in the 24h operations list
4. **If operation is not found** (outside 24h window), it continues without action
5. Operation remains permanently in status, causing rule to stay "InProgress"

### Root Cause
The `getOperations()` method uses a hardcoded 24-hour lookback window:
```kusto
.show operations | where StartedOn > ago(1d) | ...
```

Operations that were submitted more than 24 hours ago cannot be found, even though they may still exist in Kusto's operations history and can be queried individually.

### Issue Impact
- SummaryRules stuck in "InProgress" state indefinitely
- No resolution for operations outside the 24-hour window
- Accumulation of unreachable operations in status messages

## Identification of the Bug

**Location**: `ingestor/adx/tasks.go:527-560` in `getOperations()` and `ingestor/adx/tasks.go:446-452` in `trackAsyncOperations()`

**Bug Code**:
```go
// getOperations() - hardcoded 24h limitation
stmt := kql.New(".show operations | where StartedOn > ago(1d) | ...")

// trackAsyncOperations() - searches only in 24h results
index := slices.IndexFunc(kustoAsyncOperations, func(item AsyncOperationStatus) bool {
    return item.OperationId == op.OperationId
})

if index == -1 {
    continue  // ❌ BUG: Operations outside 24h window are ignored forever
}
```

**Expected Behavior**: Operations with `OperationId` should be queryable directly regardless of age.

**Actual Behavior**: Operations are ignored indefinitely when they age out of the 24-hour bulk query window.

## Recommended Fix

### Strategy
Replace the bulk `getOperations()` method with individual `getOperation()` lookups that can query for specific operations by ID without time-based limitations. This eliminates the 24-hour lookbook window issue entirely while preserving all existing functionality.

### Implementation Approach
1. **Add `getOperation()` method**: Query for specific operations by OperationId using parameterized queries
2. **Modify `trackAsyncOperations()`**: Replace bulk operation lookup with individual queries
3. **Preserve backlog operations**: Ensure operations with empty `OperationId` continue normal processing
4. **Maintain existing logic**: Preserve retry and completion handling for found operations

### Key Benefits
- **Eliminates 24-hour limitation**: Can find operations regardless of age
- **Better performance**: Only queries for operations we actually need to track
- **Cleaner logic**: Direct lookup instead of searching filtered bulk results
- **Preserves all functionality**: No changes to backlog operation handling

### Validation Strategy
The fix will be validated through:
- Unit tests that verify individual operation lookup functionality
- Integration tests confirming operations can be found regardless of age
- Verification that backlog operations (empty `OperationId`) are unaffected
- Performance testing to ensure individual queries don't impact system performance

## Task Breakdown

### Task 1: Implement Individual Operation Lookup Method
**Commit Message**: `feat: add getOperation method for individual operation lookup`

**⚠️ SURGICAL IMPLEMENTATION REQUIRED ⚠️**
- **ONLY** add the `getOperation` method - no other changes
- **DO NOT** modify any existing methods in this task
- **VERIFY** the method signature exactly matches the plan

**Implementation Details**:
- Add `getOperation(ctx context.Context, operationId string) (*AsyncOperationStatus, error)` method to `SummaryRuleTask`
- Use parameterized KQL query to prevent injection: `.show operations | where OperationId == @ParamOperationId`
- Return `*AsyncOperationStatus` or `nil` if not found
- Add proper error handling for query failures
- Include defensive parsing and validation

**Exact Implementation Pattern**:
```go
func (t *SummaryRuleTask) getOperation(ctx context.Context, operationId string) (*AsyncOperationStatus, error) {
	stmt := kql.New(`
		.show operations
		| where OperationId == @ParamOperationId  
		| summarize arg_max(LastUpdatedOn, OperationId, State, ShouldRetry, Status)
		| project LastUpdatedOn, OperationId = tostring(OperationId), State, ShouldRetry = todouble(ShouldRetry), Status
	`)
	params := kql.NewParameters().AddString("ParamOperationId", operationId)
	
	rows, err := t.kustoCli.Mgmt(ctx, stmt, kusto.QueryParameters(params))
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve operation %s: %w", operationId, err)
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
			return nil, fmt.Errorf("failed to retrieve operation %s: %v", operationId, errFinal)
		}

		var status AsyncOperationStatus
		if err := row.ToStruct(&status); err != nil {
			return nil, fmt.Errorf("failed to parse operation %s: %v", operationId, err)
		}
		if status.State != "" {
			return &status, nil
		}
	}

	// Operation not found
	return nil, nil
}
```

**Files to modify**: `ingestor/adx/tasks.go`

### Task 2: Replace Bulk Query with Individual Lookups
**Commit Message**: `fix: replace bulk getOperations with individual getOperation lookups`

**⚠️ CRITICAL: PRESERVE BACKLOG OPERATIONS ⚠️**
- **VERIFY** operations with empty `OperationId` still go to `processBacklogOperation()`
- **DO NOT** change the logic for empty `OperationId` operations
- **ONLY** replace the bulk lookup with individual calls

**Implementation Details**:
- Modify `trackAsyncOperations()` to call `getOperation()` for each operation with `OperationId`
- Remove dependency on `getOperations()` bulk query results
- Preserve existing retry and completion logic
- **Critical**: Ensure operations with empty `OperationId` continue to `processBacklogOperation()`
- Add proper error handling for individual query failures

**Exact Logic Change**:
```go
// BEFORE (current):
index := slices.IndexFunc(kustoAsyncOperations, func(item AsyncOperationStatus) bool {
    return item.OperationId == op.OperationId
})

if index == -1 {
    continue
}

kustoOp := kustoAsyncOperations[index]

// AFTER (new):
kustoOp, err := t.getOperation(ctx, op.OperationId)
if err != nil {
    logger.Errorf("Failed to query operation %s: %v", op.OperationId, err)
    continue
}
if kustoOp == nil {
    // Operation not found - could be old or completed
    continue  
}
```

**Files to modify**: `ingestor/adx/tasks.go` (trackAsyncOperations method)

### Task 3: Update Initialization and Remove Bulk Query
**Commit Message**: `refactor: remove unused getOperations bulk query method`

**⚠️ VERIFY DEPENDENCIES BEFORE REMOVAL ⚠️**
- **CHECK** that no other code calls `getOperations()` before removing
- **VERIFY** method signature changes don't break existing code

**Implementation Details**:
- Remove or deprecate `getOperations()` method
- Update `initializeRun()` to not call `getOperations()`
- Simplify method signatures that no longer need bulk operation results
- Update any remaining references to use individual lookups

**Files to modify**: `ingestor/adx/tasks.go`

### Task 4: Add Comprehensive Tests and Documentation
**Commit Message**: `test: add comprehensive tests for individual operation lookup`

**⚠️ FOCUS ON CRITICAL FUNCTIONALITY ⚠️**
- **MUST** test that backlog operations (empty `OperationId`) are unaffected
- **DO NOT** add unnecessary test complexity

**Implementation Details**:
- Add unit tests for `getOperation()` method edge cases
- Add integration tests for mixed operation scenarios (with/without OperationId)
- Test error handling and edge cases (malformed IDs, query failures)
- Update method documentation with examples
- **Critical**: Test that backlog operations (empty `OperationId`) are unaffected

**Files to modify**: `ingestor/adx/tasks_test.go`

## Implementation Notes

## Implementation Notes

### ⚠️ MANDATORY CONSTRAINTS ⚠️
- **No deviation from the plan**: Implement only what is explicitly outlined
- **Preserve existing functionality**: Especially empty `OperationId` processing
- **Surgical changes only**: Each modification must be justified and necessary
- **Follow exact patterns**: Use the provided code examples as templates

### New Method Implementation
The `getOperation()` method uses parameterized queries for security and follows the exact pattern in Task 1.

### Error Handling Patterns
- **Preserve backlog operations**: Never query for operations with empty `OperationId`
- Use defensive error handling for query failures
- Distinguish between query errors and "not found" results  
- Maintain existing error handling for Kusto operations
- Log individual operation query failures for observability

### Performance Considerations  
- **Typically better performance**: Most rules have few active operations (<10)
- **Individual queries**: More targeted than bulk query + filtering
- **Reduced data transfer**: Only retrieve operations we care about
- **Parameterized queries**: Efficient query plan caching in Kusto

### Backward Compatibility
- No changes to SummaryRule CRD schema
- No changes to operation storage format
- **Backfill functionality completely preserved** - no changes to empty `OperationId` processing
- Existing behavior preserved for all operation types
- Same retry and completion logic

### Testing Integration
- Use existing mock patterns from `tasks_test.go`
- Mock individual operation queries using `TestStatementExecutor`
- Test various operation states and edge cases
- No external dependencies or testcontainers needed
- **Critical**: Verify backlog operations (empty `OperationId`) are unaffected

## Final Verification Requirements

Before marking any task complete, the implementing agent MUST verify:

1. **Functionality Preserved**: All existing behavior works exactly as before
2. **Backlog Operations Safe**: Operations with empty `OperationId` are completely unaffected
3. **Performance Maintained**: No degradation in typical scenarios
4. **Error Handling**: Proper error handling for all edge cases
5. **Test Coverage**: Comprehensive tests covering the specific changes made

**Any deviation from this plan or addition of "nice to have" features will be considered a failed implementation.**
