# ADX Operator Guide

> Scope: this document focuses exclusively on what the adx-mon operator does for Azure Data Explorer (ADX). Ingestor and Collector component operations are still a work-in-progress.

## What the operator provides (ADX only)

- **Zero-config bootstrap for new ADX clusters.** When you omit explicit Azure settings the operator pulls subscription, resource group, and region from Azure IMDS and chooses an available SKU automatically.
- **Bring-your-own cluster support.** Supply an endpoint and the operator will skip provisioning and focus on federation/metadata tasks.
- **Declarative database lifecycle.** Databases listed in the CRD are created if missing (including the heartbeat database when federation is enabled). Databases omitted from the spec are left untouched.
- **Heartbeat-driven federation.** Partition clusters publish their discoverable schema and partition metadata to a hub. The hub turns that information into ADX tables and macro-expand functions that fan out across partitions.
- **Status tracking and resumable reconciliation.** Conditions on the CRD capture progress (`Creating`, `Waiting`, `ClusterReady`, `Complete`, `PermissionRestricted`). Reconciliation resumes automatically after Azure operations finish.
- **Safe interaction with partially managed resources.** 403 responses from Azure management APIs are tolerated: provisioning is skipped, but federation continues to operate.
- **No implicit cleanup.** Deleting the CRD will not delete Azure assets; the operator intentionally leaves clusters and databases in place.

## Prerequisites

### Kubernetes

- adx-mon operator installed in your cluster with the `ADXCluster` CRD registered.
- The namespace where you create `ADXCluster` objects must allow the operator’s ServiceAccount to read/write those CRDs.

### Azure access

- The operator authenticates using `DefaultAzureCredential`. Provide a workload identity, pod-managed identity, service principal, or other supported credential chain that can:
  - Register the `Microsoft.Kusto` provider.
  - Create or update resource groups, ADX clusters, and databases when you request provisioning.
  - Execute Kusto management commands ( `.show`, `.create table`, `.execute database script`, `.ingest inline` ) against every cluster endpoint referenced in your manifests.
- For partition clusters, the identity used to write heartbeats must have ingestion rights on the federated cluster(s). The operator uses the `managedIdentityClientId` value from each federation target to request a user-assigned identity token when connecting to the hub.

### Networking & endpoints

- Every `endpoint` value must resolve from the operator’s pod network. HTTPS endpoints are expected and are treated as ADX data-plane URIs.
- When zero-config provisioning is used, the resulting endpoint is written back into the CRD (`spec.endpoint`) once the cluster becomes reachable.

## ADXCluster specification anatomy

### Core cluster fields

| Field | Required | Notes |
| --- | --- | --- |
| `spec.clusterName` | ✅ | Azure resource name for the ADX cluster. Must match `^[a-zA-Z0-9-]+$` and be ≤100 characters. |
| `spec.endpoint` | ⛔ (optional) | Provide the HTTPS URI of an existing cluster to skip provisioning. |
| `spec.criteriaExpression` | ⛔ | CEL expression evaluated against operator cluster labels; returning `false` pauses reconciliation without error. |

### Database list

`spec.databases` is an array of objects with the fields below. The operator creates each database if it does not already exist.

| Field | Required | Behavior |
| --- | --- | --- |
| `databaseName` | ✅ | Must match `^[a-zA-Z0-9_]+$`. |
| `telemetryType` | ✅ | One of `Metrics`, `Logs`, `Traces`. Used for intent tagging only; no downstream behavior today. |
| `retentionInDays` | ⛔ | Schema default is 30, but the current controller release does not push retention settings to Azure. |
| `retentionPolicy` | ⛔ | Present for future use; not applied yet. |

When you omit the entire list the operator injects two default entries: `Metrics` (Metrics telemetry) and `Logs` (Logs telemetry).

### Azure provisioning block

Only required when you are asking the operator to create an ADX cluster.

| Field | Required when provisioning? | Behavior |
| --- | --- | --- |
| `subscriptionId` | 🔄 auto-detected | Pulled from IMDS when omitted. |
| `resourceGroup` | 🔄 auto-detected/created | Discovered from IMDS; if missing the operator creates it before cluster creation. |
| `location` | 🔄 auto-detected | Populated via IMDS. Must be a region where your identities have rights. |
| `skuName` | 🔄 auto-selected | If you omit it, the operator inspects available SKUs and prefers `Standard_L8as_v3`, `Standard_L16as_v3`, then `Standard_L32as_v3`. If none are listed, the first Standard SKU reported by the API is used. |
| `tier` | 🔄 defaulted | Defaults to `Standard` when not supplied. |
| `userAssignedIdentities` | ⛔ | Array of resource IDs for user-assigned managed identities. If you leave it empty the operator runs the cluster with a system-assigned identity. |
| `autoScale`, `autoScaleMin`, `autoScaleMax` | ⛔ | Applied during cluster creation/update when `autoScale` is `true`. Defaults are `false`, `2`, and `10` respectively. |
| `appliedProvisionState` | (read-only) | JSON snapshot maintained by the operator so it can detect when you change SKU, tier, or managed identities. Do not edit manually. |

### Federation section

Use `spec.role` to opt into federation features. Valid values are `Partition` and `Federated`.

#### Partition clusters (`spec.role: Partition`)

- `spec.federation.federatedTargets[]` — list of hub connections:
  - `endpoint`: hub ADX endpoint.
  - `heartbeatDatabase`: database in the hub where heartbeats should land.
  - `heartbeatTable`: target table name (created on the hub if missing).
  - `managedIdentityClientId`: client ID of a user-assigned identity that has ingest permissions on the hub.
- `spec.federation.partitioning` — free-form key/value map describing the partition (geo, tenant, SKU, anything you want). The map is serialized into heartbeats as `PartitionMetadata`.

#### Federated clusters (`spec.role: Federated`)

- `spec.federation.heartbeatDatabase` — hub database that stores incoming heartbeats.
- `spec.federation.heartbeatTable` — table name for heartbeats.
- `spec.federation.heartbeatTTL` — how far back to read heartbeats (defaults to `1h`). Accepts Kusto timespan strings (e.g., `4h30m`).

## Reconciliation lifecycle

The controller tracks progress with the condition type `adxcluster.adx-mon.azure.com` and the reasons listed below.

### Creation phase

Triggered when a new `ADXCluster` appears or when no status condition exists.

1. **Defaults applied (`applyDefaults`).**
   - Populates provisioning fields via IMDS when absent.
   - Chooses an ADX SKU when you do not specify one.
   - Seeds `spec.databases` with `Metrics` and `Logs` when empty.
   - Saves any changes back to the Kubernetes API.
2. **Provider registration.** Ensures `Microsoft.Kusto` is registered in the target subscription. If registration is in progress the controller requeues after five minutes.
3. **Resource group check.** Creates the resource group if it does not already exist.
4. **Cluster create/update.**
   - Requests system-assigned identity unless you provided user-assigned identities.
   - Enables engine type V3 and disables auto-stop.
   - Applies optimized autoscale settings when requested.
   - Requeues for one minute while Azure provisions the cluster.
5. **Database ensure.** For every database in the spec (plus the heartbeat database when running as a federated hub) the controller:
   - Checks existence via `.show databases`.
   - Falls back to the Azure ARM API to create databases that are missing.
   - Requeues for one minute if any database creation was kicked off.
6. **Heartbeat table ensure.** When the cluster is a federated hub, the controller runs `.create table <heartbeat>` with schema `Timestamp: datetime, ClusterEndpoint: string, Schema: dynamic, PartitionMetadata: dynamic`.
7. **Status update.** Reason changes to `Waiting` while the cluster transitions to the running state.

### Status checks (`CheckStatus`)

Once Azure reports the cluster as `Running`:

- The condition is set to `ClusterReady` with status `True`.
- `spec.provision.appliedProvisionState` is updated to reflect the effective SKU, tier, and managed identities.
- `spec.endpoint` is populated with the cluster URI returned by Azure.
- The CRD itself is updated so future reconciliations use the concrete endpoint.

If provisioning fails (`ProvisioningState == Failed`) the controller reports a `False` condition and stops retrying until you change the spec.

### Updates (`UpdateCluster`)

When you edit the CRD:

- SKU, tier, and user-assigned identities are compared to the stored `appliedProvisionState`.
- Differences trigger an Azure `BeginCreateOrUpdate` call (the control plane API handles patch semantics).
- The controller marks the condition reason as `Waiting` while the update runs, then cycles back through status checks.
- If Azure returns `403 Forbidden`, the condition switches to `PermissionRestricted` (status `True`). Provisioning changes are skipped, but federation logic continues to operate.

### Deletion

When you delete the CRD the controller logs the event but leaves Azure resources untouched.

## Federation workflow

### Partition side (`HeartbeatFederatedClusters`)

Runs every 10 minutes when `spec.role` is `Partition` and `federation` is configured.

1. Connects to the partition cluster using the default Azure credential chain.
2. Enumerates databases via `.show databases`.
3. For each database:
   - Captures tables from `.show tables`.
   - Captures functions from `.show functions`; each function is inspected to see if it is a Kusto view (`FunctionKind == "ViewFunction"`).
4. Builds a JSON payload per database with three lists: database name, tables, and view names.
5. Serializes the `partitioning` map into `PartitionMetadata`.
6. Uses CSV inline ingestion to write a single row to each federated target’s heartbeat table with columns:
   - `Timestamp` (RFC3339 string)
   - `ClusterEndpoint` (the partition’s endpoint from `spec.endpoint`)
   - `Schema` (JSON array of `ADXClusterSchema` objects)
   - `PartitionMetadata` (JSON map)
7. When `managedIdentityClientId` is provided and the hub endpoint uses HTTPS, the controller authenticates to the hub using that user-assigned identity.

If a heartbeat write fails for a target the error is logged, but the loop continues for the remaining targets.

### Federated hub side (`FederateClusters`)

Runs every 10 minutes when `spec.role` is `Federated`.

1. Ensures the heartbeat table described earlier exists.
2. Reads the heartbeat table with the query `<heartbeatTable> | where Timestamp > ago(<heartbeatTTL>)`.
3. Parses each row into:
   - `schemaByEndpoint`: slice of databases/tables/views per partition endpoint.
   - `partitionMetaByEndpoint`: map of partition metadata (currently collected but not yet used elsewhere).
4. Ensures every database referenced by partitions exists on the hub (using the same `ensureDatabases` logic described earlier).
5. Calls `collectInventoryByDatabase` to merge tables and view names across partitions. For each database it then:
   - Creates missing tables with the schema `Timestamp, ObservedTimestamp, TraceId, SpanId, SeverityText, SeverityNumber, Body, Resource, Attributes` (all OTLP-friendly types).
   - Note: view names are included in this pass and result in hub tables with the OTLP schema of the same name.
6. Builds a mapping of table → list of partition endpoints (`mapTablesToEndpoints`). Only table names participate in this fan-out map; views do not.
7. Generates `.create-or-alter function <table>() { macro-expand entity_group [cluster('<endpoint>').database('<db>'), ...] as X { X.<table> } }` for every table.
8. Groups definitions into scripts no larger than 1 MB and executes them via `.execute database script with (ContinueOnErrors=true)`.

The result is a hub database that contains:

- OTLP-shaped tables for every discovered table or view.
- Up-to-date macro-expand functions that aggregate each table across all contributing partition clusters.

## Identity and credential handling

- **Cluster provisioning:** Azure management clients authenticate with `DefaultAzureCredential`.
- **Kusto management commands (hub & partition):**
  - HTTPS endpoints use `DefaultAzureCredential`.
  - Federated target connections use `WithUserManagedIdentity(<managedIdentityClientId>)` when provided.
- Ensure your identities have the necessary control-plane and data-plane permissions before creating the CRD. Missing rights will surface as reconciliation errors.

## Status signals and requeue cadence

| Trigger | Outcome |
| --- | --- |
| Provider registration pending | Condition reason `Creating`; requeue after 5 minutes. |
| Cluster provisioning/updates in progress | Condition reason `Waiting`; requeue after 1 minute. |
| Partition heartbeat/federation loops | Requeue after 10 minutes between runs. |
| 403 from Azure management API | Condition reason `PermissionRestricted` (status `True`). Federation keeps running. |
| Provisioning failure | Condition status `False`, reason set to the Azure provisioning state. |

Inspect the CRD status (`kubectl get adxcluster <name> -o yaml`) to monitor progress.

## Current limitations and behaviors to be aware of

- Retention settings (`retentionInDays`, `retentionPolicy`) are accepted by the API but are not yet applied to ADX databases.
- Partition metadata gathered from heartbeats is stored but not consumed by the controller.
- View names discovered from partitions result in hub tables with OTLP schema, but macro-expand functions are generated only for base table names.
- The operator never deletes clusters, databases, or heartbeat tables, even if you remove them from the spec.
- Heartbeats rely on inline ingestion; if the hub throttles or rejects ingestion the partition will log an error and retry on the next 10-minute cycle.

## Putting it into practice

### Example: register an existing ADX hub

```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: demo-hub
spec:
  clusterName: demo-hub
  endpoint: "https://demo-hub.kusto.windows.net"
  role: Federated
  federation:
    heartbeatDatabase: FleetDiscovery
    heartbeatTable: Heartbeats
    heartbeatTTL: 2h
```

Apply it with `kubectl apply -f hub.yaml`. Because an `endpoint` is provided the operator assumes the cluster already exists; it immediately marks the CRD ready and waits for federation loops to begin. Ensure the hub already has the listed databases or grant the operator permission and provisioning details so later federation passes can create missing assets.

### Example: connect a partition to the hub

```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: demo-partition-1
spec:
  clusterName: demo-partition-1
  endpoint: "https://demo-partition-1.kusto.windows.net"
  role: Partition
  federation:
    partitioning:
      geo: "westeurope"
      tenant: "acme"
    federatedTargets:
      - endpoint: "https://demo-hub.kusto.windows.net"
        heartbeatDatabase: "FleetDiscovery"
        heartbeatTable: "Heartbeats"
        managedIdentityClientId: "11111111-2222-3333-4444-555555555555"
  databases:
    - databaseName: Metrics
      telemetryType: Metrics
    - databaseName: Logs
      telemetryType: Logs
```

In this scenario the partition cluster already exists. The operator will skip provisioning, but every ten minutes it will enumerate the partition’s schema and ingest a heartbeat row into the hub’s `Heartbeats` table using the supplied user-assigned identity. The hub reconciliation loop will then mirror any newly discovered tables into OTLP-shaped hub tables and refresh the macro-expand functions.

## Troubleshooting tips

- Watch the controller logs to see detailed Azure or Kusto errors during reconciliation.
- Check the CRD status conditions for reason messages and timestamps.
- Use `kubectl describe adxcluster <name>` to surface recent events.
- For federation, confirm that records are appearing in the heartbeat table: `https://dataexplorer.azure.com/` → run `Heartbeats | take 10` in the hub database.
- If heartbeats are present but tables/functions are missing, verify the hub identity can run `.create table` and `.execute database script` commands.

---

Because every statement above is derived from the current controller implementation (`operator/adx.go` and `api/v1/adxcluster_types.go`), this README reflects the exact behavior you should expect from the ADX side of the adx-mon operator today.