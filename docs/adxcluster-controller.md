# ADXCluster Controller

## Purpose
Reconciles `ADXCluster` CRDs to provision or connect Azure Data Explorer clusters, configure federation between them, and expose stable endpoints for other adx-mon components (ingestor, collector, alerter). The controller ships inside the operator deployment, so every reconciliation shares lifecycle, logging, and configuration with the rest of the control-plane services.

## Core Capabilities
- **Dual mode**: Bring-your-own clusters (`spec.endpoint` only) or full Azure provisioning (`spec.provision` block). Presence of `spec.provision` is the only trigger for provisioning; omitting it keeps the cluster in BYO mode.
- **Explicit configuration**: Validates all required fields up front; never injects defaults or mutates the spec
- **Federation**: Partition clusters heartbeat schema to federated hubs; hubs generate cross-cluster query functions so downstream services can issue single logical queries against multi-cluster fleets.
- **Status tracking**: Records resolved endpoint and applied provisioning state for change detection and downstream consumption
- **Conditional execution**: `spec.criteriaExpression` gates reconciliation based on operator cluster labels

## Prerequisites

### Azure Identity & Permissions
Authenticates via `azidentity.NewDefaultAzureCredential`. When both system-assigned and user-assigned identities exist on the operator pod, the controller prefers explicit identity IDs from the CRD (e.g., `spec.provision.userAssignedIdentities`) and falls back to the pod-managed identity. Required permissions:
- **Subscription**: `Microsoft.Resources/subscriptions/providers/read` (Kusto provider registration)
- **Resource Group**: read/write (`Microsoft.Resources/subscriptions/resourcegroups/*`)
- **ADX Cluster/Database**: read/write (`Microsoft.Kusto/clusters/*`, `Microsoft.Kusto/clusters/databases/*`)
- **Kusto Data Plane**: Execute management commands (`.show`, `.create table`, `.execute database script`, `.ingest inline`)

For partition clusters, the managed identity specified in `federatedTargets[].managedIdentityClientId` needs ingest permissions on the hub's heartbeat database.

### Networking
All `endpoint` values must be resolvable and reachable (HTTPS) from the operator pod. Ensure outbound egress rules (Calico, NSGs, firewalls, Private Link) allow connections from the operator namespace to ADX endpoints.

## Reconciliation Flow
With prerequisites satisfied, the controller walks through the following reconciliation flow:
1. **Criteria evaluation**: If `spec.criteriaExpression` is present, evaluate against operator cluster labels supplied via the operator's `--cluster-label` flag. False/error → update status and stop.
2. **Deletion**: CRD deletion does not touch Azure resources.
3. **Mode selection**:
    - No `spec.provision` → expect existing cluster at `spec.endpoint` (condition: `ProvisioningSkipped` or `ProvisioningDisabled`, where *Skipped* means BYO mode and *Disabled* means the criteria expression stopped reconciliation)
    - `spec.provision` present → validate all required fields (subscriptionId, resourceGroup, location, skuName, tier). Missing → `ProvisioningInvalid`
4. **Provisioning** (when `spec.provision` is valid):
    - Register `Microsoft.Kusto` provider (requeue 5min if pending)
    - Ensure resource group exists
    - Create/update cluster with system-assigned identity (unless `userAssignedIdentities` specified) and optional autoscale (`cluster.Properties.OptimizedAutoscale` in ARM)
    - Create databases from `spec.databases` + heartbeat database (if federation enabled)
    - Condition: `Creating` → `Waiting` (requeue 1min)
5. **Status polling**: Check Azure cluster state. When `StateRunning` → record endpoint/settings in status, condition: `ClusterReady`
    - HTTP 403 during polling → `PermissionRestricted` (status `True`), federation proceeds. Most often this indicates the managed identity lacks Kusto data-plane rights; grant the `Kusto Data Reader` role on the target database and requeue.
6. **Updates**: Compare desired SKU/tier/identities vs `status.appliedProvisionState`. Only apply spec changes; ignore Azure drift to avoid reconciliation loops—manually reconcile Azure-side edits that diverge from the CRD. Identity type switching not supported.
7. **Federation loops** (when `ClusterReady`):
   - **Partition** (`HeartbeatFederatedClusters`): Every 10min, enumerate schema → ingest to hub heartbeat table
   - **Federated** (`FederateClusters`): Every 10min, read heartbeats → ensure hub tables/functions for cross-cluster queries

Status conditions use type `adxcluster.adx-mon.azure.com` with reasons: `ProvisioningDisabled`, `ProvisioningSkipped`, `ProvisioningInvalid`, `Creating`, `Waiting`, `ClusterReady`, `PermissionRestricted`, `CriteriaExpressionError`, `CriteriaExpressionFalse`, or Azure failure states. All include `ObservedGeneration`.


## Spec Fields

### `clusterName` (required)
Azure resource name. Regex: `^[a-zA-Z0-9-]+$`, max 100 chars.

### `endpoint`
HTTPS URI of existing ADX cluster. When set, provisioning is skipped and value is mirrored to `status.endpoint`.

### `databases`
List of databases to create (only when `spec.provision` is populated). Each needs `databaseName` (`^[a-zA-Z0-9_]+$`) and `telemetryType` (Metrics/Logs/Traces). Retention fields are accepted for forward compatibility but currently ignored by the controller. Plus heartbeat database when federation enabled. For BYO clusters, create databases manually in Azure and configure retention there.

### `provision`
Azure provisioning details. All required when present: `subscriptionId`, `resourceGroup`, `location`, `skuName`, `tier`. Optional: `userAssignedIdentities`, `autoScale`, `autoScaleMin`, `autoScaleMax`. 
- Autoscale only applied at creation; changes require manual Azure updates
- Identity type switching not supported
- `appliedProvisionState` in status tracks last reconciled SKU/tier/identities

### `role`
Federation mode: `Partition` (local storage + heartbeat to hub) or `Federated` (aggregate partitions). Omit for standalone.

### `federation`
**Partition clusters** need:
- `federatedTargets[]`: each with `endpoint`, `heartbeatDatabase`, `heartbeatTable`, `managedIdentityClientId`
- `partitioning`: free-form map (e.g., `{geo: EU, tenant: acme}`) sent in heartbeats. Common keys mirror operator labels such as `geo`, `tenant`, or `environment` so hubs can route traffic predictably.

**Federated clusters** need:
- `heartbeatDatabase`, `heartbeatTable`: where partitions write
- `heartbeatTTL`: freshness window (default `1h`)

#### Blocked partition list (Federated hubs)
`spec.federation.blockedClusters` lets hub clusters exclude quarantined or misbehaving partitions from macro generation
without editing the partition CRDs. The controller merges two sources into a normalized block list (lower-cased, trimmed
of whitespace and trailing slashes):

- `blockedClusters.static`: literal list of partition endpoints to suppress.
- `blockedClusters.kustoFunction`: optional break-glass function looked up in the specified `database`/`name`. The
  function must return a tabular result with a string column named `ClusterEndpoint` (preferred) or `Endpoint`. Missing
  functions are treated as warnings; other errors halt the reconciliation so ops teams can inspect the failure.

After fetching heartbeats the controller filters any partitions whose endpoints match the block list, logs how many
entries were removed vs. matched, and proceeds with macro creation using the remaining schema.

### `criteriaExpression`
Optional CEL expression against operator cluster labels. Empty/missing = `true`. Errors/false → reconciliation blocked. Example: `labels["geo"] == "eu" && labels.has("tier")` keeps the object scoped to European, tiered operators. See the [CEL language spec](https://opensource.google/projects/cel) for expression syntax.


## Federation

### Partition Clusters
Every 10min: enumerate local databases/tables/views → serialize with `partitioning` map → ingest CSV row to each hub's heartbeat table.

Heartbeat row (CSV): `Timestamp` (RFC3339), `ClusterEndpoint`, `Schema` (JSON array of `{database, tables[], views[]}`), `PartitionMetadata` (JSON of partitioning map).

Authentication: `DefaultAzureCredential` for local cluster; switches to `managedIdentityClientId` for hub ingestion when both endpoint is HTTPS and identity specified.
Failures to push heartbeats surface in the operator logs (`adxcluster-controller` logger) alongside the CRD name; reconcile once connectivity or permissions are restored.

### Federated Hubs  
Every 10min: query heartbeat table (`WHERE Timestamp > ago(heartbeatTTL)`) → extract partition schemas → optionally
filter out any endpoints listed in `spec.federation.blockedClusters` (static values plus any returned by the Kusto function) → ensure OTLP hub tables exist → generate cross-cluster functions. Filtering happens before schema aggregation so blocked partitions contribute neither tables nor functions.

Heartbeat table schema: `Timestamp:datetime, ClusterEndpoint:string, Schema:dynamic, PartitionMetadata:dynamic`

Hub tables: OTLP schema `Timestamp:datetime, ObservedTimestamp:datetime, TraceId:string, SpanId:string, SeverityText:string, SeverityNumber:int, Body:dynamic, Resource:dynamic, Attributes:dynamic`

Hub tables inherit ADX defaults for retention; set custom policies post-creation if required (the controller does not mutate them).

Federation functions: `.create-or-alter function <table>() { macro-expand entity_group [cluster(...).database(...), ...] as X { X.<table> } }` — generated only for base tables (views excluded). Scripts split at 1MB. The `entity_group` macro fans a single logical function across all remote databases discovered in heartbeats, so callers can query the hub without enumerating partitions.

Hubs auto-create databases discovered in heartbeats without mutating the CRD.


## Examples
The following snippets highlight common configurations and can be pasted directly into a manifest for quick starts.

### Provision New Cluster
Provision an entire ADX environment, including databases, through `spec.provision`.
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: observability-eastus2
spec:
  clusterName: observability-eastus2
  provision:
    subscriptionId: "00000000-0000-0000-0000-000000000000"
    resourceGroup: "observability-eastus2"
    location: "eastus2"
    skuName: "Standard_L8as_v3"
    tier: "Standard"
    autoScale: true
    autoScaleMin: 2
    autoScaleMax: 10
  databases:
    - databaseName: "Metrics"
      telemetryType: Metrics
    - databaseName: "Logs"
      telemetryType: Logs
```

### Connect Existing Cluster
Reference a pre-existing ADX cluster and skip provisioning logic.
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: shared-adx
spec:
  clusterName: shared-adx
  endpoint: "https://shared-adx.kusto.windows.net"
```

### Partition Cluster
Advertise a regional partition that emits heartbeats to one or more hubs.
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: eu-partition
spec:
  clusterName: eu-partition
  endpoint: "https://eu-partition.kusto.windows.net"  # or use provision block
  databases:
    - databaseName: "Metrics"
      telemetryType: Metrics
  role: Partition
  federation:
    federatedTargets:
      - endpoint: "https://hub.kusto.windows.net"
        heartbeatDatabase: "FleetDiscovery"
        heartbeatTable: "Heartbeats"
        managedIdentityClientId: "22222222-2222-2222-2222-222222222222"
    partitioning:
      geo: "EU"
      tenant: "contoso"
```

### Federated Hub
Assemble schemas emitted by partitions and expose macro functions for fleet-wide queries.
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: global-hub
spec:
  clusterName: global-hub
  endpoint: "https://global-hub.kusto.windows.net"  # or use provision block
  role: Federated
  federation:
    heartbeatDatabase: "FleetDiscovery"
    heartbeatTable: "Heartbeats"
    heartbeatTTL: "2h"
    blockedClusters:
      static:
        - "https://rogue-partition.kusto.windows.net"
      kustoFunction:
        database: FleetDiscovery
        name: GetBlockedPartitions
```

The static list handles known quarantined clusters, while the `GetBlockedPartitions()` function can emit emergent
endpoints discovered by external tooling. The controller merges both sources and keeps deduplicated, normalized entries
for filtering during macro reconciliation.

### Gate Reconciliation with Criteria
Restrict reconciliation to operators that carry matching labels.
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: shared-eu
spec:
  clusterName: shared-eu
  endpoint: "https://shared-eu.kusto.windows.net"
  criteriaExpression: 'labels["geo"] == "eu" && labels.has("tier")'
```


## Operational Notes
- **No deletion**: Removing CRD never deletes Azure resources (clusters, databases, identities)
- **Database management**: Only created when `spec.provision` populated; BYO clusters require manual database setup
- **Retention policies**: `retentionInDays`/`retentionPolicy` fields accepted but not applied; set in Azure directly
- **Autoscale updates**: Only applied at creation; later changes require manual Azure edits
- **Identity switching**: System ↔ user-assigned not supported; Azure must already be in target mode
- **Change detection**: Only spec edits trigger updates; Azure drift ignored to prevent thrashing—audit Azure activity logs and reconcile by hand if out-of-band edits are necessary.
- **HTTP 403 handling**: Status polling sets `PermissionRestricted` (federation continues); provisioning APIs fail immediately. Grant the managed identity (from `spec.provision` or `federation`) the `Kusto Data Reader` or richer role on the affected database.
- **Logs**: Controller logs are emitted by the operator deployment (`adxcluster-controller` logger). Use `kubectl logs` against the operator pod to trace reconciliation IDs or heartbeat failures.

## Troubleshooting
- **Check status**: `kubectl describe adxcluster <name>` shows conditions, `ObservedGeneration`, error messages. `ProvisioningDisabled`, `ProvisioningSkipped`, and `ClusterReady` correspond to the states outlined in the reconciliation flow.
- **Provisioning failures**: Look for `ProvisioningInvalid` or Azure state (e.g., `Failed`) in conditions. Azure Activity Logs (Portal → Monitor → Activity Log) reveal authoritative ARM failures.
- **Database issues**: Check operator logs; no dedicated condition for database creation failures. Most errors surface as reconciliation warnings with the database name.
- **Federation debugging**: Query heartbeat table: `<heartbeatTable> | where Timestamp > ago(2h)` to confirm heartbeat freshness and partition metadata.
- **Force requeue**: Update or annotate the CRD (`kubectl annotate --overwrite adxcluster/<name> adx-mon.azure.com/requeue=$(date +%s)`) to nudge a new reconciliation once prerequisites are fixed.
- **Kusto errors**: Verify network connectivity, authentication, and required permissions, especially data-plane roles for managed identities listed in the CRD.

