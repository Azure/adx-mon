# SummaryRule Backfill Feature Implementation Plan

## Planning Progress
- [x] 🔍 Feature requirements gathered
- [x] 📚 Codebase patterns analyzed  
- [x] 🌐 Third-party research complete
- [x] 🏗️ Architecture decisions made
- [x] 💬 User validation complete
- [x] 📋 Implementation plan finalized

## Feature Overview
This plan outlines the implementation of a backfill feature for SummaryRules that allows users to specify a date range for historical data processing. The feature will enable SummaryRules to process historical data by automatically advancing through time windows from a specified start datetime until reaching an end datetime.

## Context Research Summary

### Current SummaryRule Architecture
I've analyzed the existing SummaryRule implementation and identified these key patterns:

**Current Execution Flow:**
1. `SummaryRuleTask.Run()` processes each rule individually
2. `shouldProcessRule()` filters by database and criteria matching
3. `handleRuleExecution()` determines if a rule should submit based on interval timing
4. `NextExecutionWindow()` calculates time ranges based on last execution
5. Async operations are tracked via Kusto operation IDs in CRD status conditions

**Key Components:**
- `SummaryRuleSpec`: Contains interval, database, table, body (KQL), and criteria
- `AsyncOperation`: Tracks individual query submissions with OperationId, StartTime, EndTime
- Time substitution via `kustoutil.ApplySubstitutions()` using `_startTime` and `_endTime` variables
- Uses RFC3339Nano format for datetime values in Kusto queries

**Current Timing Logic:**
- Rules execute on fixed intervals from last successful execution 
- `BackfillAsyncOperations()` already exists to catch up missed intervals
- Time windows are aligned to interval boundaries with optional ingestion delay

### Third-Party Research Findings
**Kusto DateTime Handling:**
- Supports ISO 8601 formats (RFC3339 compatible)
- Uses UTC time zone exclusively
- Minimum time unit is 100 nanoseconds ("ticks")
- Prefers `datetime(2024-01-01T00:00:00Z)` format for literals

## User Requirements Analysis

Based on user feedback:

### **Backfill Specification:**
```yaml
spec:
  backfill:
    start: "2024-01-01T00:00:00Z"  # Required start datetime
    end: "2024-01-31T23:59:59Z"    # Required end datetime
```

### **Key Design Decisions:**
1. **Parallel Execution**: Backfill operations run alongside normal interval execution
2. **Auto-cleanup**: System removes `Backfill` field when complete
3. **Reconcile Integration**: Backfill as standard reconcile operation (submit interval, then backfill if needed)
4. **Interval Alignment**: Use same interval boundaries and `IngestionDelay` patterns
5. **Validation**: Start < End, max 30-day lookback period
6. **User Responsibility**: Users handle data overlap concerns

### **Critical Design Question: Operation Tracking**

## Architectural Decision: Single Backfill Operation Tracking

**Implementation Strategy:**
We will track backfill progress by storing the current operation directly in the spec:

```yaml
spec:
  backfill:
    start: "2024-01-01T00:00:00Z"
    end: "2024-01-31T23:59:59Z"
    operationId: "some-async-operation-id"  # Current operation
```

**Rationale:**
- **Prevents Kusto Overload**: Serial execution avoids overwhelming Kusto with expensive backfill queries
- **Steady Progress**: Reconcile loop (1-minute intervals) ensures consistent advancement
- **Simple State Management**: Single operation tracking is straightforward to implement and debug
- **Natural Completion**: When `start >= end`, backfill is complete and field can be removed
- **Clear Progress Visibility**: Users can easily see current backfill progress in the spec

**Implementation Details:**
```go
type BackfillSpec struct {
    Start       string `json:"start"`       // Current backfill position
    End         string `json:"end"`         // Target end time
    OperationId string `json:"operationId,omitempty"` // Active operation ID
}
```

**Execution Flow:**
1. If `operationId` is empty and `start < end`: Submit next backfill window, store operation ID
2. If `operationId` exists: Check operation status in Kusto
3. If operation completes successfully: Advance `start` to window end time, clear `operationId`
4. If operation fails: Handle error, clear `operationId` for retry
5. If `start >= end`: Remove entire `backfill` field from spec

## Risk Assessment Matrix

### **Overall Feature Risks:**
- **CRD Update Risk**: Breaking changes to SummaryRule API
  - *Mitigation*: Use optional fields with proper validation
- **Spec Mutation Conflicts**: User editing spec during backfill
  - *Mitigation*: Document behavior, use generation tracking
- **Performance Impact**: Backfill operations affecting normal processing
  - *Mitigation*: Serial execution prevents overload

### **Integration Risks:**
- **Kusto Operation Tracking**: Backfill operations not tracked in AsyncOperations
  - *Mitigation*: Query Kusto directly using operation ID
- **Time Window Calculation**: Incorrect interval alignment
  - *Mitigation*: Reuse existing `NextExecutionWindow` patterns
- **Validation Edge Cases**: Invalid date ranges, timezone issues
  - *Mitigation*: Comprehensive validation with proper error messages

## Implementation Plan

### **Task 1: Add Backfill Spec to SummaryRule CRD**
**Commit Message**: `feat: add backfill specification to SummaryRule CRD`

**Objective**: Extend SummaryRule API to support backfill operations

**Implementation Details:**
- Add `BackfillSpec` struct to `api/v1/summaryrule_types.go`
- Add optional `Backfill *BackfillSpec` field to `SummaryRuleSpec` 
- Add kubebuilder validation tags for date format and max lookback period
- Add const for max backfill lookback period (30 days)
- Run `make generate-crd CMD=update` to generate kubebuilder manifests after modifying the spec.

**Files to modify:**
- `api/v1/summaryrule_types.go`

**Validation Criteria:**
- CRD accepts valid backfill specifications
- Validation rejects invalid date ranges and excessive lookback periods
- Existing SummaryRules continue to work unchanged

**Review Guidance:**
- Focus on validation logic correctness
- Ensure backwards compatibility

---

### **Task 2: Add Backfill Helper Methods**
**Commit Message**: `feat: add backfill helper methods to SummaryRule`

**Objective**: Implement core backfill logic methods on SummaryRule type

**Implementation Details:**
- Add `HasActiveBackfill() bool` method
- Add `GetNextBackfillWindow(clock.Clock) (time.Time, time.Time, bool)` method
- Add `AdvanceBackfillProgress(endTime time.Time)` method
- Add `ClearBackfillOperation()` method
- Add `IsBackfillComplete() bool` method

**Files to modify:**
- `api/v1/summaryrule_types.go`

**Testing:**
- Unit tests for all helper methods with various scenarios
- Test boundary conditions (completion, invalid states)

**Validation Criteria:**
- Methods correctly calculate next backfill windows
- Progress advancement works with interval alignment
- Completion detection is accurate

---

### **Task 3: Integrate Backfill into SummaryRuleTask**
**Commit Message**: `feat: integrate backfill execution into SummaryRuleTask`

**Objective**: Modify task execution to handle backfill operations

**Implementation Details:**
- Add `handleBackfillExecution(ctx, rule)` method to `SummaryRuleTask`
- Modify `Run()` method to call backfill handler after normal execution
- Add backfill operation status checking in `trackAsyncOperations()`
- Implement backfill completion cleanup

**Files to modify:**
- `ingestor/adx/tasks.go`

**Integration Notes:**
- Reuse existing `submitRule()` method for backfill operations
- Use same Kusto operation polling infrastructure
- Ensure backfill doesn't interfere with normal operation tracking

**Validation Criteria:**
- Backfill operations submit correctly
- Progress advances after successful operations
- Failed operations retry appropriately
- Backfill completes and cleans up properly

---

### **Task 4: Add Comprehensive Backfill Unit Tests**
**Commit Message**: `test: add comprehensive unit tests for backfill functionality`

**Objective**: Ensure backfill feature works correctly across all scenarios

**Implementation Details:**
- Test backfill window calculation with various intervals
- Test progress advancement and completion detection
- Test error handling and retry scenarios  
- Test integration with normal rule execution
- Test edge cases (invalid dates, completed backfills)

**Files to modify:**
- `api/v1/summaryrule_types_test.go`
- `ingestor/adx/tasks_test.go`

**Testing Coverage:**
- All backfill helper methods
- Integration with SummaryRuleTask
- Error conditions and edge cases
- Clock-dependent behavior with fake clocks

---

### **Task 5: Update CRD Manifests**
**Commit Message**: `feat: regenerate CRD manifests for backfill support`

**Objective**: Update generated CRD YAML files with backfill fields

**Implementation Details:**
- Run `make generate-crd CMD=update` to regenerate manifests
- Verify generated YAML includes proper validation rules
- Update operator manifests

**Files to modify:**
- `kustomize/bases/adx-mon.azure.com_summaryrules.yaml`
- `operator/manifests/crds/adx-mon.azure.com_summaryrules.yaml`

**Validation Criteria:**
- Generated CRDs include backfill fields
- Validation rules are properly applied
- No breaking changes to existing fields

---

### **Task 6: Add Documentation and Examples**
**Commit Message**: `docs: add backfill feature documentation and examples`

**Objective**: Document the backfill feature for users

**Implementation Details:**
- Add backfill section to SummaryRule documentation
- Create example YAML manifests showing backfill usage
- Document limitations and best practices

**Files to modify:**
- `docs/crds.md` (or relevant SummaryRule documentation)
- Create example files in appropriate documentation location

**Documentation Coverage:**
- Backfill field specification
- Example use cases
- Limitations and performance considerations
- Troubleshooting common issues

## Implementation Notes

### **Error Handling Patterns:**
- Use existing Kusto error parsing via `kustoutil.ParseError()`
- Log backfill progress at Info level for visibility
- Handle operation failures with same retry logic as normal operations

### **Configuration:**
- Max backfill lookback period: 30 days (const in CRD types file)
- Backfill operations use same `IngestionDelay` as normal operations
- Interval alignment follows existing `NextExecutionWindow` patterns

### **Backward Compatibility:**
- All backfill fields are optional
- Existing SummaryRules work unchanged
- No migration required for existing deployments

### **Performance Considerations:**
- Serial backfill execution prevents Kusto overload
- Backfill operations use same async operation infrastructure
- Progress visibility through spec inspection

## Documentation Plan

### **Existing Documentation Updates:**
- Update `docs/crds.md` to document backfill field
- Add backfill examples to existing SummaryRule documentation

### **New Documentation:**
- Backfill troubleshooting guide
- Performance impact documentation
- Best practices for large date ranges
