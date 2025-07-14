# Kusto SDK Migration Plan

## Overview
This document tracks the migration plan for updating the Azure Kusto Go SDK from our current forked version to the latest official release (v1.0.3).

## Current State
- We're using a forked version: `github.com/jwilder/azure-kusto-go v0.15.3-0.20240403192022-0d7016e79525`
- This replaces `github.com/Azure/azure-kusto-go v0.15.2` in our go.mod
- Latest5. Set up development branch for migration work
6. Begin with Phase 1 (dependency updates)
7. Execute checklist phases in order
8. Conduct thorough testing at each phase
9. Document any issues discovered during migration

## Validation Summary

This migration plan has been validated against the actual codebase and includes:

### Confirmed Current Usage Patterns
✅ **Authentication**: `WithUserManagedIdentity()` usage in `alerter/multikustoclient/auth.go`  
✅ **Query Building**: `kusto.NewStmt()` pattern in `alerter/engine/context.go`  
✅ **Result Iteration**: `iter.Do()` pattern in `alerter/multikustoclient/client.go`  
✅ **Ingestion**: `ingest.New(client, db, table)` pattern in `ingestor/adx/uploader.go`  
✅ **Error Handling**: `kustoerrors.HttpError` wrapping in `pkg/kustoutil/errors.go`

### Mixed API State Discovered  
⚠️ Some files already use `kql.New()` (tasks.go) while others use `kusto.NewStmt()` (context.go)  
⚠️ Suggests fork has partial v1.0 features backported  

### File Count Validation
✅ 56+ files confirmed to require updates across 8 major components  
✅ All critical files identified and included in phase-by-phase checklist  

### Dependencies Validated
✅ Current: `github.com/jwilder/azure-kusto-go v0.15.3-0.20240403192022-0d7016e79525`  
✅ Target: `azkustodata/v1.0.3` and `azkustoingest/v1.0.3` (latest available)

The plan is comprehensive and ready for execution.cial version: `azkustodata/v1.0.3` (released after v1.0.0 breaking changes)
- We need to migrate from pre-v1.0.0 API to v1.0.3

## Important Discovery

**Mixed API Usage**: Investigation reveals that some files are already using the newer `kql.New()` API pattern while still importing from the old package structure. This suggests our fork may have backported some v1.0 features.

Files using **NEW** patterns:
- `ingestor/adx/tasks.go` - Uses `kql.New()` for management commands

Files using **OLD** patterns:  
- `alerter/engine/context.go` - Uses `kusto.NewStmt()` 
- `alerter/multikustoclient/client.go` - Uses `iter.Do()` instead of `DoOnRowOrError()`

This mixed state means migration complexity may be lower than initially estimated for some components.

### Major Package Restructuring
- **Module Split**: SDK split into two modules:
  - `azkustodata` - querying, management APIs
  - `azkustoingest` - ingestion operations
- **Import Path Changes**: No more single `github.com/Azure/azure-kusto-go/kusto` import

### Authentication & Connection Changes  
- **KCSB Changes**: Kusto Connection String Builder now case-insensitive
- **Certificate Methods**: `WithApplicationCertificate` removed, replaced with:
  - `WithAppCertificatePath` - for certificate files
  - `WithAppCertificateBytes` - for in-memory certificates
- **Managed Identity**: `WithUserManagedIdentity` removed, replaced with:
  - `WithUserAssignedIdentityClientId`
  - `WithUserAssignedIdentityResourceId`

### Query API Changes
- **New Query API**: Completely new querying approach
- **Stmt Builder**: `kusto.NewStmt` removed, use `kql.New()` instead  
- **Iterator Changes**: `DoOnRowOrError` pattern instead of `Do`
- **Management Commands**: Use `client.Mgmt()` instead of special query client

### Type System Changes
- **Nullable Types**: Now return pointers for nullable values (except strings)
- **Decimal Values**: Now `decimal.Decimal` instead of `string`
- **Dynamic Type**: Returns `[]byte` of JSON, user must unmarshal

### Ingestion Changes
- **Client Creation**: Use `KustoConnectionStringBuilder` instead of client structs
- **Endpoint Inference**: Managed streaming infers ingest endpoint from query endpoint
- **Stream Methods**: Old `Stream()` method removed, use `NewStreaming()` or `NewManaged()`
- **Default DB/Table**: No longer required in constructor, use options instead

### Minimum Requirements
- **Go Version**: Now requires Go 1.22+

## Current Usage Analysis

### Import Patterns Used
Our codebase extensively uses the Kusto SDK across multiple components:

**Primary Kusto client:**
- `github.com/Azure/azure-kusto-go/kusto` - Main client (16 files)

**Data handling:**
- `github.com/Azure/azure-kusto-go/kusto/data/table` - Result tables (9 files)
- `github.com/Azure/azure-kusto-go/kusto/data/errors` - Error handling (5 files)
- `github.com/Azure/azure-kusto-go/kusto/data/value` - Data values (5 files)
- `github.com/Azure/azure-kusto-go/kusto/data/types` - Type system (3 files)

**Query building:**
- `github.com/Azure/azure-kusto-go/kusto/kql` - Query language (7 files)

**Ingestion:**
- `github.com/Azure/azure-kusto-go/kusto/ingest` - Data ingestion (2 files)

**Low-level:**
- `github.com/Azure/azure-kusto-go/kusto/unsafe` - Unsafe operations (5 files)

### Components Affected
1. **Ingestor** (`ingestor/adx/`): All files use Kusto for data ingestion - uses `ingest.New(n.KustoCli, n.database, table)` pattern
2. **Alerter** (`alerter/`): Engine, multikustoclient, and main service - uses custom `iter.Do()` pattern for query results
3. **ADX Exporter** (`adxexporter/`): Query execution
4. **Operator** (`operator/`): ADX management operations  
5. **Metrics** (`metrics/`): Service operations
6. **Transform** (`transform/`): Data value conversion - uses `kusto/data/value` types
7. **Test Utils** (`pkg/testutils/`): Integration testing - custom `Uploader` implements `ingest.Ingestor`
8. **Utilities** (`pkg/kustoutil/`): Error handling utilities - wraps `kustoerrors.HttpError`

### Critical Current Patterns That Will Break
- **Auth**: `WithUserManagedIdentity(msi)` → `WithUserAssignedIdentityClientId(msi)`  
- **Query Building**: `kusto.NewStmt("", kusto.UnsafeStmt(...)).UnsafeAdd(query)` → `kql.New(query)`
- **Ingestion**: `ingest.New(client, db, table)` → `azkustoingest.New(connectionString, options...)`
- **Result Iteration**: `iter.Do(func(row *table.Row) error)` → `iter.DoOnRowOrError(func(row *table.Row, e *errors.Error) error)`
- **Management Queries**: Currently differentiated by `IsMgmtQuery` flag, will use `client.Mgmt()` vs `client.Query()`

## Files to Update

### Core Import Changes (56+ files)
All files using Kusto imports need updates:

**Main Client Usage:**
- `cmd/ingestor/main.go` - Client creation and auth
- `alerter/service.go` - Main alerter service  
- `operator/adx.go` - Operator ADX operations
- `adxexporter/kusto.go` - Exporter client
- `metrics/service.go` - Metrics service

**Data Access & Processing:**
- `ingestor/adx/` (9 files) - All ingestion logic
- `alerter/multikustoclient/` (4 files) - Multi-client handling
- `alerter/engine/` (7 files) - Query execution engine  
- `transform/kusto_to_metrics.go` - Data transformation
- `pkg/kustoutil/` (2 files) - Error handling utilities

**Testing Infrastructure:**
- `pkg/testutils/` (5 files) - Integration test helpers
- All `*_test.go` files using Kusto (15+ files)

### Go Module Updates
- `go.mod` - Replace forked dependency with new modules
- `go.sum` - Regenerate checksums

### Test & Build Infrastructure  
- Integration tests requiring Kusto credentials
- CI/CD pipelines that run integration tests
- Docker images that may cache old dependencies

## Migration Plan

### Phase 1: Dependency Update
1. **Update go.mod**: Replace fork with official v1.0.3 modules
2. **Add new dependencies**: Both `azkustodata` and `azkustoingest`
3. **Remove old dependency**: Clean up forked version reference

### Phase 2: Import Updates (56+ files)
1. **Client imports**: `kusto` → `azkustodata`
2. **Ingestion imports**: `kusto/ingest` → `azkustoingest` 
3. **Data imports**: Update all `kusto/data/*` imports to `azkustodata/data/*`
4. **KQL imports**: `kusto/kql` → `azkustodata/kql` 
5. **Unsafe imports**: `kusto/unsafe` → `azkustodata/unsafe`

**Note**: Some imports like `kusto/data/table`, `kusto/data/errors`, `kusto/data/value`, `kusto/data/types` will move to the `azkustodata` package structure.

### Phase 3: API Migration
1. **Connection Building**: 
   - `kusto.NewConnectionStringBuilder()` → `azkustodata.NewConnectionStringBuilder()`
   - `kusto.New()` → `azkustodata.New()`
2. **Query Building**:
   - `kusto.NewStmt()` → `kql.New()`  
   - Update query parameter handling
3. **Query Execution**:
   - `client.Query()` API changes
   - `client.Mgmt()` for management commands
   - Update result iteration patterns
4. **Ingestion**:
   - `ingest.New()` → `azkustoingest.New()` with KCSB
   - Update streaming ingestion clients
   - Migration endpoint inference logic
5. **Rule Statement Compilation**: 
   - `alerter/rules/store.go` - Rule.Stmt field type change from `kusto.Stmt` to new statement type
   - Query compilation in `alerter/engine/context.go` needs full rewrite

### Phase 4: Type System Updates
1. **Nullable Types**: Handle pointer returns for nullable values
2. **Decimal Types**: Convert from `string` to `decimal.Decimal`
3. **Dynamic Types**: Update JSON `[]byte` handling
4. **Error Types**: Ensure error handling still works

### Phase 5: Testing Updates
1. **Integration Tests**: Update all Kusto integration tests
2. **Mock/Fake**: Update test doubles and mocks
3. **Test Utilities**: Update helper functions in `pkg/testutils/`
4. **Verification**: Ensure test coverage maintained

## Testing Strategy

### Pre-Migration Testing
1. **Baseline Tests**: Run full test suite to establish baseline
2. **Integration Test Documentation**: Document current integration test behavior  
3. **Performance Baseline**: Capture current query/ingestion performance metrics

### Migration Testing Approach
1. **Incremental Migration**: Migrate component by component, not all at once
2. **Parallel Testing**: Keep both old and new implementations during transition
3. **Component Isolation**: Test each component's migration independently

### Post-Migration Validation
1. **Functional Testing**: All existing functionality works with new SDK
2. **Integration Testing**: Full end-to-end workflow testing
3. **Performance Testing**: Ensure no regressions in query/ingestion performance
4. **Compatibility Testing**: Verify all ADX features still work as expected

### Test Coverage Areas
- **Alerter Engine**: Query execution and result processing  
- **Ingestor**: WAL segment upload and streaming ingestion
- **ADX Exporter**: Data export queries
- **Operator**: Cluster management operations
- **Error Handling**: All error conditions still handled properly
- **Authentication**: All auth methods still work
- **Multi-cluster**: Multiple Kusto endpoint handling

## Technical Challenges & Considerations

### High-Risk Areas
1. **Authentication Patterns**: Current `WithUserManagedIdentity()` usage in `auth.go` will break - requires migration to `WithUserAssignedIdentityClientId()`
2. **Multi-cluster Logic**: `alerter/multikustoclient/` handles multiple endpoints  
3. **Error Handling**: `pkg/kustoutil/errors.go` wraps `kustoerrors.HttpError` - types may have changed
4. **Performance Critical**: `ingestor/adx/` is in the hot path for metrics ingestion
5. **Unsafe Operations**: 5 files use `kusto/unsafe` - behavior may have changed
6. **Query Building**: Uses `kusto.NewStmt()` pattern in `alerter/engine/context.go` - needs migration to `kql.New()`
7. **Iterator Pattern**: Uses `iter.Do()` pattern - needs migration to `DoOnRowOrError()`

### Component-Specific Risks
- **Ingestor WAL Segments**: Critical for data durability, ingestion must not break
- **Alerter Query Engine**: Complex query execution logic with templating  
- **Multi-tenant**: Different databases/clusters may have different auth requirements
- **Integration Tests**: Heavy dependency on Azure credentials and test infrastructure
- **Rule Compilation**: `alerter/rules/store.go` has Stmt field that will need type change
- **Authentication Breaking Change**: `MsiAuth()` function in `multikustoclient/auth.go` uses deprecated `WithUserManagedIdentity()` - will cause immediate auth failures

### Migration Strategy Considerations
- **Minimal Changes**: Change only what's necessary for SDK compatibility
- **Backward Compatibility**: Ensure external APIs remain the same
- **Performance**: New SDK may have different performance characteristics
- **Dependencies**: New SDK may pull in additional dependencies

### Deployment Considerations  
- **Rolling Updates**: Can we update components independently?
- **Monitoring**: Need to monitor for new error patterns post-migration
- **Rollback**: Must be able to quickly rollback if issues discovered in production

## Specific Code Pattern Changes

### Authentication Migration
```go
// OLD (alerter/multikustoclient/auth.go:30)
return kcsb.WithUserManagedIdentity(msi)

// NEW  
return kcsb.WithUserAssignedIdentityClientId(msi)
```

### Query Building Migration
```go
// OLD (alerter/engine/context.go:49)
stmt := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(query)

// NEW
stmt := kql.New(query) // Much simpler, but parameters handling will need rework
```

### Result Iteration Migration  
```go
// OLD (alerter/multikustoclient/client.go:66)
if err := iter.Do(func(row *table.Row) error {
    // handle row
    return fn(ctx, client.Endpoint(), qc, row)
}); err != nil {

// NEW
if err := iter.DoOnRowOrError(func(row *table.Row, e *errors.Error) error {
    if e != nil {
        return e
    }
    // handle row  
    return fn(ctx, client.Endpoint(), qc, row)
}); err != nil {
```

### Ingestion Client Migration
```go
// OLD (ingestor/adx/uploader.go:171)
ingestor, err = ingest.New(n.KustoCli, n.database, table)

// NEW  
kcsb := azkustodata.NewConnectionStringBuilder(endpoint).WithDefaultAzureCredential()
ingestor, err = azkustoingest.New(kcsb, azkustoingest.WithDefaultDatabase(n.database), azkustoingest.WithDefaultTable(table))
```

## Execution Checklist

### Prerequisites
- [ ] Document current test results for baseline comparison
- [ ] Identify all integration tests that use Kusto  
- [ ] Capture current performance metrics
- [ ] Review all current authentication patterns used

### Phase 1: Dependency Update
- [ ] Update `go.mod` to remove forked dependency `github.com/jwilder/azure-kusto-go v0.15.3-0.20240403192022-0d7016e79525`
- [ ] Add `github.com/Azure/azure-kusto-go/azkustodata v1.0.3`
- [ ] Add `github.com/Azure/azure-kusto-go/azkustoingest v1.0.3`  
- [ ] Remove old `github.com/Azure/azure-kusto-go v0.15.2` dependency
- [ ] Run `go mod tidy` to clean up dependencies
- [ ] Verify go.sum updated correctly
- [ ] Check for any transitive dependency conflicts

### Phase 2: Core Client Migration
- [ ] Update `cmd/ingestor/main.go` - client creation and auth (`newKustoClient` function)
- [ ] Update `alerter/service.go` - main service client
- [ ] Update `operator/adx.go` - operator operations
- [ ] Update `adxexporter/kusto.go` - exporter client  
- [ ] Update `metrics/service.go` - metrics service
- [ ] **CRITICAL**: Update `alerter/multikustoclient/auth.go` - fix `WithUserManagedIdentity()` → `WithUserAssignedIdentityClientId()`
- [ ] Test each component individually after migration

### Phase 3: Data Layer Migration  
- [ ] Update `ingestor/adx/uploader.go` - ingestion client creation
- [ ] Update `ingestor/adx/syncer.go` - sync operations
- [ ] Update `ingestor/adx/tasks.go` - task execution
- [ ] Update `ingestor/adx/dispatcher.go` - dispatching logic
- [ ] Update all other `ingestor/adx/*` files
- [ ] Test ingestion functionality

### Phase 4: Query Engine Migration
- [ ] **CRITICAL**: Update `alerter/engine/context.go` - rewrite `kusto.NewStmt()` pattern to `kql.New()`
- [ ] Update `alerter/engine/executor.go` - query execution
- [ ] Update `alerter/engine/worker.go` - worker logic and result iteration pattern
- [ ] Update `alerter/engine/client.go` - client interface  
- [ ] Update `alerter/multikustoclient/client.go` - `iter.Do()` → `iter.DoOnRowOrError()` pattern
- [ ] **CRITICAL**: Update `alerter/rules/store.go` - change Rule.Stmt field type
- [ ] Test alert query execution

### Phase 5: Utilities & Transform Migration
- [ ] Update `transform/kusto_to_metrics.go` - data transformation
- [ ] Update `pkg/kustoutil/errors.go` - error handling utilities
- [ ] Update `alerter/rules/store.go` - rule storage
- [ ] Test data transformation pipelines

### Phase 6: Test Infrastructure Migration  
- [ ] Update `pkg/testutils/kql_verify.go` - verification helpers
- [ ] Update `pkg/testutils/uploader.go` - test upload helpers
- [ ] Update `pkg/testutils/integration_test.go` - integration tests
- [ ] Update `pkg/testutils/kustainer/*` - test container setup
- [ ] Update all fake/mock implementations
- [ ] Run subset of integration tests to verify test infrastructure

### Phase 7: Individual Test File Migration
- [ ] Update `ingestor/adx/*_test.go` files (6 files)
- [ ] Update `alerter/engine/*_test.go` files (4 files)  
- [ ] Update `alerter/multikustoclient/*_test.go` files (1 file)
- [ ] Update `operator/adx_test.go`
- [ ] Update `adxexporter/kusto_test.go`
- [ ] Update `transform/kusto_to_metrics_test.go`
- [ ] Update `pkg/kustoutil/errors_test.go`

### Phase 8: Integration & Validation
- [ ] Run full test suite: `go test ./...`
- [ ] Run integration tests: `make test`  
- [ ] Performance test comparison with baseline
- [ ] End-to-end workflow testing
- [ ] Verify all authentication methods work
- [ ] Test multi-cluster configurations

### Phase 9: Documentation & Cleanup
- [ ] Update any internal documentation referencing old SDK
- [ ] Clean up any temporary migration code
- [ ] Update CI/CD if needed for new dependencies
- [ ] Document any behavior changes discovered
- [ ] Update this design document with lessons learned

### Rollback Plan
- [ ] Keep the fork reference in go.mod commented for easy rollback
- [ ] Document exact rollback steps if migration fails
- [ ] Identify rollback testing approach
- [ ] Plan communication strategy if rollback needed

## Summary

This migration from our forked Azure Kusto Go SDK (v0.15.2) to the official v1.0.3 release represents a significant but necessary update to align with the official SDK and gain access to the latest features and bug fixes.

### Key Migration Facts
- **Scope**: 56+ files across 8 major components need updates
- **Complexity**: High - breaking changes in core APIs, authentication, and type system
- **Risk**: Medium-High - affects critical data ingestion and alerting pipelines
- **Timeline**: Estimated 2-3 weeks for complete migration and testing

### Success Criteria
- [ ] All existing functionality works with new SDK
- [ ] No performance regressions in critical paths  
- [ ] All integration tests pass
- [ ] All authentication methods continue to work (especially MSI auth)
- [ ] WAL segment ingestion maintains reliability
- [ ] Alert query execution remains accurate
- [ ] No breaking changes to external APIs exposed by our services
- [ ] Query parameter handling still works correctly
- [ ] Multi-cluster/multi-database scenarios work

### Next Steps
1. Get approval for migration plan
2. Set up development branch for migration work
3. Begin with Phase 1 (dependency updates)
4. Execute checklist phases in order
5. Conduct thorough testing at each phase
6. Document any issues discovered during migration
