# Bugfix Plan: OneTick Subtraction Missing in BackfillAsyncOperations

## Summary
The `BackfillAsyncOperations` method in `api/v1/summaryrule_types.go` is missing the `OneTick` subtraction from `windowEnd` that is properly applied in `handleRuleExecution`. This inconsistency can cause overlapping time windows between backfilled operations and regular operations.

## Analysis of the Problem

### Current Inconsistency
In `ingestor/adx/tasks.go::handleRuleExecution`:
```go
// Subtract 1 tick (100 nanoseconds) from endTime for the query
queryEndTime := windowEndTime.Add(-kustoutil.OneTick)

asyncOp := v1.AsyncOperation{
    StartTime: windowStartTime.Format(time.RFC3339Nano),
    EndTime:   queryEndTime.Format(time.RFC3339Nano),  // ✅ Uses OneTick-adjusted time
}
```

In `api/v1/summaryrule_types.go::BackfillAsyncOperations`:
```go
newOp := AsyncOperation{
    OperationId: "", // Empty for backlog operations
    StartTime:   windowStart.Format(time.RFC3339Nano),
    EndTime:     windowEnd.Format(time.RFC3339Nano),    // ❌ Missing OneTick subtraction
}
```

### Root Cause
The `BackfillAsyncOperations` method was implemented without the same boundary adjustment logic used in regular operation submission, creating potential for overlapping time windows.

### Issue Impact
- **Potential data duplication**: Overlapping windows could cause the same data to be processed twice
- **Boundary issues**: KQL queries using `between(_startTime .. _endTime)` may include boundary data incorrectly
- **Inconsistent behavior**: Different time window calculations between regular and backfilled operations

## Identification of the Bug

**Location**: `api/v1/summaryrule_types.go:377-381` in `BackfillAsyncOperations()`

**Bug Code**:
```go
newOp := AsyncOperation{
    OperationId: "", // Empty for backlog operations
    StartTime:   windowStart.Format(time.RFC3339Nano),
    EndTime:     windowEnd.Format(time.RFC3339Nano),  // ❌ Missing OneTick subtraction
}
```

**Expected Behavior**: Should subtract `OneTick` from `windowEnd` before formatting, consistent with regular operations.

**Actual Behavior**: Uses raw `windowEnd` time, potentially creating overlapping windows.

## Recommended Fix

### Strategy
Apply the same `OneTick` subtraction pattern used in `handleRuleExecution` to the `BackfillAsyncOperations` method. This is a minimal surgical fix that ensures consistency across operation creation paths.

### Implementation Approach
1. **Import required package**: Add `kustoutil` import to `summaryrule_types.go`
2. **Apply OneTick subtraction**: Subtract `kustoutil.OneTick` from `windowEnd` before formatting
3. **Update tests**: Ensure existing tests account for the OneTick adjustment
4. **Verify consistency**: Confirm both code paths now use identical time boundary logic

## Task Breakdown

### Task 1: Add Test Demonstrating Inconsistent Behavior
**Commit Message**: `test: add test demonstrating OneTick inconsistency between regular and backfill operations`

**Objective**: Create a test that shows the current inconsistency in time window boundaries between regular operations and backfilled operations.

**Implementation Details**:
- Create test that generates both regular and backfilled operations for same time window
- Compare the EndTime values between regular and backfilled operations
- Demonstrate that backfilled operations have EndTime that's OneTick higher
- Test should fail initially, showing the inconsistency

**Files to modify**:
- `api/v1/summaryrule_types_test.go`

**Testing**: Test should fail with current implementation

**Validation Criteria**: Test demonstrates OneTick difference between operation types

### Task 2: Add kustoutil Import and Apply OneTick Fix
**Commit Message**: `fix: apply OneTick subtraction to BackfillAsyncOperations EndTime`

**Objective**: Fix the inconsistency by applying OneTick subtraction to backfilled operations' EndTime.

**Risk Assessment**: 
- **Low risk**: Brings consistency with existing proven pattern
- **Data safety**: Prevents potential overlapping windows
- **Boundary correctness**: Ensures proper boundary handling in KQL queries

**Implementation Details**:
- Add import for `github.com/Azure/adx-mon/pkg/kustoutil`
- Modify `windowEnd` calculation to subtract `OneTick` before formatting
- Add comment explaining the OneTick subtraction for boundary handling

**Files to modify**:
- `api/v1/summaryrule_types.go`

**Integration Notes**: Uses existing `kustoutil.OneTick` constant

**Testing**: Previously failing test should now pass

**Validation Criteria**: Backfilled operations now have consistent EndTime with regular operations

### Task 3: Update Existing Tests for OneTick Change
**Commit Message**: `test: update BackfillAsyncOperations tests for OneTick adjustment`

**Objective**: Update existing tests to account for the OneTick subtraction in backfilled operations.

**Implementation Details**:
- Review all existing tests in `summaryrule_types_test.go` that verify BackfillAsyncOperations
- Update test expectations to account for OneTick subtraction in EndTime
- Ensure tests properly validate the boundary adjustment
- Add additional test cases for edge scenarios

**Files to modify**:
- `api/v1/summaryrule_types_test.go`

**Testing**: All existing tests should pass with updated expectations

**Validation Criteria**: Tests properly validate OneTick-adjusted EndTime values

### Task 4: Add Documentation Comment
**Commit Message**: `docs: add comment explaining OneTick subtraction in BackfillAsyncOperations`

**Objective**: Document the OneTick subtraction behavior for future maintainers.

**Implementation Details**:
- Add detailed comment explaining why OneTick is subtracted
- Reference consistency with handleRuleExecution method
- Explain boundary handling for KQL between() operations
- Make the pattern clear for future developers

**Files to modify**:
- `api/v1/summaryrule_types.go`

**Documentation Tasks**: Improve code documentation with boundary handling explanation

**Validation Criteria**: Clear documentation of OneTick subtraction rationale

## Implementation Notes

### Import Requirements
- Add `github.com/Azure/adx-mon/pkg/kustoutil` to imports in `summaryrule_types.go`
- No additional dependencies required

### Code Change
```go
// Before:
EndTime: windowEnd.Format(time.RFC3339Nano),

// After:  
EndTime: windowEnd.Add(-kustoutil.OneTick).Format(time.RFC3339Nano),
```

### Testing Strategy
- Use existing test patterns from `summaryrule_types_test.go`
- Focus on time boundary calculations
- Verify consistency between operation creation methods
- No external dependencies needed

### Performance Considerations
- Minimal performance impact (single time calculation adjustment)
- No additional memory usage
- Same computational complexity

### Backward Compatibility
- Change affects new backfilled operations only
- Existing operations in status remain unchanged
- Improves correctness without breaking changes
- May affect time windows that were previously overlapping (improvement)

## Reference Documentation
- `ingestor/adx/tasks.go::handleRuleExecution`: Reference implementation with OneTick
- `pkg/kustoutil/kql.go`: OneTick constant definition and documentation  
- `api/v1/summaryrule_types_test.go`: Existing test patterns
- Kusto documentation on datetime boundary handling with `between()` operator
