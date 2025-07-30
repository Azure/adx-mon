# ADX Operator: Named Entity-Groups for Federated Clusters

## Problem Statement

The ADX operator for federated clusters currently creates cross-cluster functions using the `macro-expand` operator with inline entity groups. While this approach works well, there is an opportunity to enhance the federation capabilities by also providing named entity-groups as an additional abstraction layer.

**Proposed Enhancement**: Implement named entity-groups that are stored in the database metadata and automatically maintained by the operator. These entity-groups will represent logical collections of partition clusters for each database, providing an additional way for users to query across federated data with a simpler syntax when desired.

## Current Architecture Analysis

Based on `operator/adx.go`, the federation system works as follows:

### Partition Clusters (Role: "Partition")
- Send heartbeats every 10 minutes via `HeartbeatFederatedClusters()` 
- Collect schema metadata (databases, tables, views) from their local cluster
- Send this data to federated clusters via CSV ingest into heartbeat tables
- Heartbeat data includes: timestamp, cluster endpoint, schema JSON, partition metadata

### Federated Clusters (Role: "Federated") 
- Receive heartbeats in heartbeat table (schema: `Timestamp: datetime, ClusterEndpoint: string, Schema: dynamic, PartitionMetadata: dynamic`)
- Query heartbeat table every 10 minutes via `FederateClusters()`
- Create cross-cluster functions using `macro-expand` with inline entity groups
- Current function generation in `generateKustoFunctionDefinitions()`:
  ```kusto
  .create-or-alter function TableName() { 
    macro-expand entity_group [cluster('endpoint1').database('db'), cluster('endpoint2').database('db')] as X { X.TableName } 
  }
  ```

## Proposed Entity-Groups Enhancement

### New Function: `ensureEntityGroups()`

Add a new step in `FederateClusters()` between steps 5-6 to create/update named entity-groups alongside the existing function generation:

**Location**: `operator/adx.go` in `FederateClusters()` method, after `ensureDatabases()` call at line ~945

**Logic**:
1. For each database discovered from heartbeat data, create a named entity-group
2. Entity-group name pattern: `{DatabaseName}_Partitions` (e.g., `Metrics_Partitions`, `Logs_Partitions`)
3. Use `.create-or-alter entity_group` command to ensure entity-groups are updated as partition clusters change
4. Entity-group contains all active partition cluster endpoints for that database

### Additional Query Options

With named entity-groups in place, users will have multiple ways to query federated data:

**Option 1: Existing Functions (unchanged)**:
```kusto
TableName()  // Uses existing macro-expand functions with inline entity groups
```

**Option 2: Direct Entity-Group Usage (new)**:
```kusto
macro-expand Metrics_Partitions as X { X.TableName }
```

**Option 3: Advanced Custom Queries (new)**:
```kusto
// Users can create their own functions using the entity-groups
.create function MyCustomMetricsQuery() {
    macro-expand Metrics_Partitions as X { 
        X.TableName 
        | where Timestamp > ago(1h)
        | summarize count() by bin(Timestamp, 5m)
    }
}
```

### Benefits
- **Flexibility**: Provides additional query patterns while preserving existing functionality
- **Advanced Use Cases**: Power users can leverage entity-groups for custom federation logic
- **Consistency**: Entity-groups are automatically maintained as partition clusters change
- **Future-Proofing**: Creates foundation for additional federation enhancements

## Implementation Plan

### Task 1: Add Entity-Group Creation Function ✅ COMPLETED
**File**: `operator/adx.go`
**Function**: `ensureEntityGroups(ctx context.Context, client *kusto.Client, dbSet map[string]struct{}, schemaByEndpoint map[string][]ADXClusterSchema) error`

**Implementation**: ✅ COMPLETED
1. ✅ Iterate through each database in `dbSet`
2. ✅ Collect all cluster endpoints that have that database from `schemaByEndpoint`
3. ✅ Build entity reference list: `cluster('endpoint').database('dbname')` for each endpoint
4. ✅ Check if entity-group exists using `.show entity_groups`
5. ✅ Execute `.create entity_group` (new) or `.alter entity_group` (existing) as appropriate
6. ✅ Log entity-group creation/updates for observability
7. ✅ Added helper function `entityGroupExists()` for existence checking

**Location**: ✅ Inserted after line ~945 in `FederateClusters()` method

### Task 2: Keep Existing Function Generation Unchanged ✅ COMPLETED
**File**: `operator/adx.go`  
**Function**: `generateKustoFunctionDefinitions()`

**No Changes Required**: ✅ VERIFIED - The existing function generation logic remains completely unchanged to maintain backward compatibility. The current inline entity-group approach continues to work as before:

```kusto
.create-or-alter function TableName() { 
  macro-expand entity_group [cluster('endpoint1').database('db'), cluster('endpoint2').database('db')] as X { X.TableName } 
}
```

✅ **Verification Completed**: All existing queries and workflows continue to function without modification.

### Task 3: Integration with Existing System
**File**: `operator/adx.go`
**Location**: `FederateClusters()` method

**Changes**:
1. Add call to `ensureEntityGroups()` after `ensureDatabases()` (line ~945)
2. Entity-group creation runs independently of existing function generation
3. Add proper error handling and logging for entity-group operations
4. Ensure entity-group creation failures don't affect existing function generation

**Note on Reconciliation**: No additional requeuing logic is required. The existing `FederateClusters()` method already returns `ctrl.Result{RequeueAfter: 10 * time.Minute}`, which ensures entity-groups will be automatically updated every 10 minutes as partition clusters join/leave the federation. This maintains consistency with the existing function generation cycle.

### Task 4: Add Entity-Group Cleanup (Optional)
**Function**: `cleanupStaleEntityGroups()`

**Logic**:
1. Query existing entity-groups using `.show entity_groups`
2. Identify entity-groups matching pattern `*_Partitions` that no longer correspond to active databases
3. Remove stale entity-groups using `.drop entity_group`
4. Execute during `FederateClusters()` reconciliation

### Task 5: Testing and Validation
**Files**: `operator/adx_test.go` (create if doesn't exist)

**Test Cases**:
1. Entity-group creation with single database/multiple partitions
2. Entity-group updates when partition clusters added/removed  
3. Verify existing function generation remains unchanged
4. Validate entity-groups can be used directly in queries
5. Integration test with actual Kusto instance (marked with `INTEGRATION=1`)
6. Ensure entity-group failures don't impact existing functionality

### Task 6: Update Documentation
**Files**: 
- `docs/designs/operator.md` 
- `docs/concepts.md`
- `docs/crds.md` (if needed)

**Documentation Updates**:
1. **Operator Design Document** (`docs/designs/operator.md`):
   - Add new section "Entity-Groups for Federation" after the existing federation section (around line 550)
   - Document the entity-group creation process and naming convention
   - Explain the new query options available to users
   - Include examples of using entity-groups directly in custom queries

2. **Concepts Document** (`docs/concepts.md`):
   - Update the "Federation & Multi-Cluster" section (around line 457) to mention entity-groups
   - Add information about the simplified query syntax available with entity-groups
   - Include examples showing both existing function calls and new entity-group usage

3. **Content to Add**:
   ```markdown
   #### Entity-Groups for Advanced Queries
   
   The operator automatically creates named entity-groups for each database discovered from partition cluster heartbeats. These entity-groups provide an additional way to query federated data:
   
   - **Naming Convention**: `{DatabaseName}_Partitions` (e.g., `Metrics_Partitions`, `Logs_Partitions`)
   - **Automatic Maintenance**: Entity-groups are updated as partition clusters are added/removed
   - **Direct Usage**: Advanced users can use entity-groups in custom queries:
     ```kusto
     // Direct entity-group usage
     macro-expand Metrics_Partitions as X { X.TableName | where Timestamp > ago(1h) }
     
     // Custom function creation
     .create function MyMetricsQuery() {
         macro-expand Metrics_Partitions as X { 
             X.TableName 
             | summarize count() by bin(Timestamp, 5m) 
         }
     }
     ```
   
   This enhancement preserves all existing functionality while providing additional flexibility for power users.
   ```

## Code References

### Key Functions to Modify:
- `FederateClusters()` - Line 893 in `operator/adx.go`
- `generateKustoFunctionDefinitions()` - Line 1360 in `operator/adx.go`
- `executeKustoScripts()` - Line 1405 in `operator/adx.go`

### Data Structures:
- `HeartbeatRow` - Line 1273, contains cluster endpoint and schema data
- `ADXClusterSchema` - Line 879, contains database/tables/views info
- `dbTableEndpoints` map structure - built in `mapTablesToEndpoints()` Line 1340

### Kusto Client Usage Pattern:
```go
client, err := kusto.New(ep)
stmt := kql.New(".create-or-alter entity_group EntityGroupName (cluster('ep1').database('db1'), cluster('ep2').database('db2'))")
_, err = client.Mgmt(ctx, database, stmt)
```

## Success Criteria

1. **Functional**: Entity-groups are created and maintained automatically for each database
2. **Additive**: New functionality is available without affecting existing queries or functions
3. **Reliability**: Entity-group creation failures don't impact existing federation functionality
4. **Usability**: Advanced users can leverage entity-groups for custom federation queries
5. **Backward Compatibility**: All existing queries and workflows continue to work unchanged

## Deployment Considerations

- **Rollout**: Feature can be enabled gradually by updating the operator without affecting existing functionality
- **No Disruption**: Existing function generation and queries remain completely unchanged
- **Additive Value**: New entity-groups provide additional capabilities for advanced users
- **Monitoring**: Add metrics for entity-group creation/update success rates
- **Documentation**: Update operator docs to explain entity-group benefits and usage patterns for users who want to leverage this new capability
