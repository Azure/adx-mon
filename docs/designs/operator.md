# adx-mon Operator Design

## Overview

The `adx-mon operator` is a Kubernetes operator responsible for managing the lifecycle of adx-mon components (collector, ingestor, alerter) and potentially related Azure infrastructure like ADX (Azure Data Explorer) clusters. The operator uses the Azure SDK for Go to manage Azure resources when necessary.

The operator aims to provide a simple, production-ready bootstrap experience for adx-mon deployments, while supporting advanced customization for complex and federated deployments.

---

## Responsibilities

- **Azure Infrastructure Management (via ADXCluster CRD):**
  - Declaratively create and manage Azure ADX clusters and databases based on the `ADXCluster` CRD.
  - Support using existing ADX clusters via the `endpoint` field.
  - Handle Azure resource provider registration and validation.
  - Automatic resource group creation and management (optional).
  - Wait for Azure resources to reach a ready state.

- **adx-mon Component Management:**
  - Generate and apply manifests for adx-mon components (collector, ingestor, alerter) based on their respective CRDs (`Collector`, `Ingestor`, `Alerter`).
  - Manage Kubernetes resources like StatefulSets, Deployments, and Services for these components.
  - Support default container images for each component, with overrides via the `image` field in each CRD spec.
  - Allow configuration of the number of ingestor instances via `spec.replicas` in the `Ingestor` CRD.
  - Support deployment of only a subset of components (e.g., only collectors) to enable federated/multi-cluster topologies.

- **Reconciliation and Drift Detection:**
  - Watch the CRDs and the managed Kubernetes resources (e.g., StatefulSets for the ingestor).
  - If a managed resource is manually changed in a way that conflicts with the CRD spec (e.g., `replicas` count), detect the change and revert it to match the CRD's desired state.
  - Ensure the operator's desired state is always enforced in the actual cluster state.
  - **Note:** The operator primarily reconciles Kubernetes resources based on CRD changes. It does not currently reconcile drifted *Azure* resources (e.g., ADX clusters modified outside Kubernetes).

- **Incremental, Granular Reconciliation:**
  - The operator proceeds in steps, often using conditions in the CRD status to track progress (e.g., `ADXCluster` ready, `Ingestor` ready).
  - Status and conditions are updated at each step to reflect progress and issues.

- **Resource Cleanup on Deletion:**
  - When a CRD for an adx-mon component (e.g., `Ingestor`) is deleted, the operator uses Kubernetes owner references to ensure the corresponding managed Kubernetes resources (like StatefulSets) are garbage collected.
  - Deletion of an `ADXCluster` CRD does *not* automatically delete the underlying Azure resources by default.

---

## Multi-Cluster and Federation Support

- The operator supports deploying components separately for federated scenarios:
  - **Central Ingestor Cluster:** Deploys `Ingestor` components (potentially linked to `ADXCluster` resources). Receives data from remote collectors.
  - **Collector Clusters:** Deploys only `Collector` components. Requires configuration of the `ingestorEndpoint` in the `CollectorSpec` to point to the central ingestor.

---

## CRD Design

> **See also:** [CRD Reference](../crds.md) for a summary table and links to all CRDs, including advanced types (SummaryRule, Function, ManagementCommand).

The adx-mon operator manages the following Custom Resource Definitions (CRDs):
- ADXCluster
- Ingestor
- Collector
- Alerter

Each CRD is described below with its current schema and example YAML, strictly reflecting the Go source definitions.

### ADXCluster CRD

**Spec fields:**
- `clusterName` (string, required): Unique, valid name for the ADX cluster. Must match ^[a-zA-Z0-9-]+$ and be at most 100 characters. Used for resource identification and naming in Azure.
- `endpoint` (string, optional): URI of an existing ADX cluster. If set, the operator will use this cluster instead of provisioning a new one. Example: "https://mycluster.kusto.windows.net"
- `databases` (array of objects, optional): List of databases to create in the ADX cluster. Each object has:
  - `databaseName` (string, required): ADX valid database name. ^[a-zA-Z0-9_]+$, 1-64 chars.
  - `retentionInDays` (int, optional): Retention period in days. Default: 30.
  - `retentionPolicy` (string, optional): ADX retention policy.
  - `telemetryType` (string, required): One of `Metrics`, `Logs`, or `Traces`.
- `provision` (object, optional): Azure provisioning details. When this section is present, the operator performs Azure resource reconciliation. **Required fields must be supplied explicitly; optional fields have defaults as noted below.**
  - **Required fields:**
    - `subscriptionId` (string, required): Azure subscription ID the operator should use.
    - `resourceGroup` (string, required): Azure resource group that owns the ADX cluster.
    - `location` (string, required): Azure region (e.g., "eastus2").
    - `skuName` (string, required): Azure SKU (e.g., "Standard_L8as_v3").
    - `tier` (string, required): Azure ADX tier (e.g., "Standard").
  - **Optional fields (with defaults):**
    - `userAssignedIdentities` (array of strings, optional): List of MSIs to attach to the cluster (resource-ids). Default: empty list.
    - `autoScale` (bool, optional): Enable auto-scaling for the ADX cluster. Default: `false`.
    - `autoScaleMax` (int, optional): Maximum number of nodes for auto-scaling. Default: `10`.
    - `autoScaleMin` (int, optional): Minimum number of nodes for auto-scaling. Default: `2`.

**Status fields:**
- `conditions` (array, optional): Standard Kubernetes conditions.
- `endpoint` (string, optional): Observed Kusto endpoint. Mirrors `spec.endpoint` in BYO scenarios or reflects the provisioned endpoint when the operator manages the cluster.
- `appliedProvisionState` (object, optional): Snapshot of the last provisioned SKU, tier, and user-assigned identities reconciled by the controller. The operator uses this record to:
  - detect spec-driven changes (e.g., user bumps the SKU) without mutating `spec`;
  - tolerate out-of-band edits made directly in Azure by skipping updates when the live cluster no longer matches the previously applied values;
  - expose the currently effective provisioning settings to other reconcilers without forcing them to reach back into Azure.

**Minimal Example:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: minimal-adx-cluster
spec:
  clusterName: minimal-adx-cluster
```

**Full Example:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: prod-adx-cluster
spec:
  clusterName: prod-metrics
  endpoint: "https://prod-metrics.kusto.windows.net"
  databases:
    - databaseName: metricsdb
      retentionInDays: 30
      telemetryType: Metrics
    - databaseName: logsdb
      retentionInDays: 30
      telemetryType: Logs
  provision:
    subscriptionId: "00000000-0000-0000-0000-000000000000"
    resourceGroup: "adx-monitor-prod"
    location: "eastus2"
    skuName: "Standard_L8as_v3"
    tier: "Standard"
    userAssignedIdentities:
      - "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/adx-monitor-prod/providers/Microsoft.ManagedIdentity/userAssignedIdentities/identity1"
    autoScale: true
    autoScaleMax: 20
    autoScaleMin: 4
```

### Ingestor CRD

**Spec fields:**
- `image` (string, optional): Container image for the ingestor component.
- `replicas` (int32, optional): Number of ingestor replicas. Default: 1.
- `endpoint` (string, optional): Endpoint for the ingestor. If running in a cluster, this should be the service name; otherwise, the operator will generate an endpoint.
- `exposeExternally` (bool, optional): Whether to expose the ingestor externally. Default: false.
- `adxClusterSelector` (LabelSelector, required): Label selector to target ADXCluster CRDs.

**Status fields:**
- `conditions` (array, optional): Standard Kubernetes conditions.

**Example:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: Ingestor
metadata:
  name: prod-ingestor
spec:
  image: "ghcr.io/azure/adx-mon/ingestor:v1.0.0"
  replicas: 3
  endpoint: "http://prod-ingestor.monitoring.svc.cluster.local:8080"
  exposeExternally: false
  adxClusterSelector:
    matchLabels:
      app: adx-mon
```

### Collector CRD

**Spec fields:**
- `image` (string, optional): Container image for the collector component.
- `ingestorEndpoint` (string, optional): URI endpoint for the ingestor service to send data to.

**Status fields:**
- `conditions` (array, optional): Standard Kubernetes conditions.

**Example:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: Collector
metadata:
  name: prod-collector
spec:
  image: "ghcr.io/azure/adx-mon/collector:v1.0.0"
  ingestorEndpoint: "http://prod-ingestor.monitoring.svc.cluster.local:8080"
```

### Alerter CRD

**Spec fields:**
- `image` (string, optional): Container image for the alerter component.
- `notificationEndpoint` (string, required): URI where alert notifications will be sent.
- `adxClusterSelector` (LabelSelector, required): Label selector to target ADXCluster CRDs.

**Status fields:**
- `conditions` (array, optional): Standard Kubernetes conditions.

**Example:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: Alerter
metadata:
  name: prod-alerter
spec:
  image: "ghcr.io/azure/adx-mon/alerter:v1.0.0"
  notificationEndpoint: "http://alerter-endpoint"
  adxClusterSelector:
    matchLabels:
      app: adx-mon
```

---

## ADX Cluster Management (Details)

An important option for ADX clusters is to "bring your own" or utilize an existing cluster. This is accomplished by specifying the cluster's endpoint in the `ADXCluster` CRD (`spec.endpoint`). When an endpoint is provided, the operator will use the referenced existing ADX cluster rather than provisioning a new one.

### Auto-Provisioning and Zero-Config Support

The operator supports both fully specified and zero-config deployments for *new* clusters (when `spec.endpoint` is not provided):

1.  **Zero-Config Mode (via Azure SDK features, not explicitly shown in operator code provided):**
    *   Uses Azure IMDS or environment variables to automatically detect:
        *   Region
        *   Subscription ID
        *   Resource Group
    *   May automatically select an optimal SKU.
    *   Creates default databases if `spec.databases` is empty.

2.  **Infrastructure Preparation (via Azure SDK):**
    *   Automatically registers the `Microsoft.Kusto` resource provider if needed.
    *   Creates resource group if specified and doesn't exist.
    *   Validates SKU availability in the target region.

### ADX Deployment Strategy (via Azure SDK)

The operator uses the Azure SDK for Go to manage the lifecycle of ADX clusters and databases when provisioning:

1.  **Azure SDK-Based Deployment:** Creates, updates, and potentially deletes ADX clusters and databases.
2.  **Database Configuration:** Databases created based on `spec.databases`.

### Managed Identity Integration (via Azure SDK)

The operator supports managed identity configuration for provisioned clusters:

1.  **Identity Assignment:** System-assigned identity by default, or user-assigned if `spec.provision.managedIdentityClientId` is provided.
2.  **Role Management:** May handle role assignments (details depend on specific Azure SDK implementation).

### SKU Selection Strategy (via Azure SDK)

The operator likely implements an SKU selection strategy when provisioning:

1.  **Preferred SKUs:** May prioritize certain SKUs (e.g., `Standard_L8as_v3`).
2.  **Fallback Behavior:** Validates SKU availability and falls back if necessary.

### Resource Group Management (via Azure SDK)

The operator handles resource group lifecycle when provisioning:

1.  **Resource Group Creation:** Checks for existence, creates if specified and not found.

---

## Example CRDs

#### Minimal (Zero-Config Ingestor/Collector/Alerter with Existing ADX Cluster)

This example assumes an `ADXCluster` named `existing-adx-cluster` (managed separately or by another CRD) exists and has the label `app: my-adx`. It deploys the components using default images and settings, connecting them to the specified cluster.

```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: existing-adx-cluster # This CRD might just reference an existing cluster
  labels:
    app: my-adx
spec:
  clusterName: my-existing-cluster-name # Name in Azure
  endpoint: "https://my-existing-cluster.eastus2.kusto.windows.net" # IMPORTANT: Tells operator to use existing
  databases:
    - databaseName: "MetricsDB"
      telemetryType: Metrics
    - databaseName: "LogsDB"
      telemetryType: Logs
# No 'provision' section needed if using existing endpoint

---

apiVersion: adx-mon.azure.com/v1
kind: Ingestor
metadata:
  name: minimal-ingestor
spec:
  adxClusterSelector:
    matchLabels:
      app: my-adx # Selects the ADXCluster above

---

apiVersion: adx-mon.azure.com/v1
kind: Collector
metadata:
  name: minimal-collector
spec: {} # Ingestor endpoint will be auto-discovered if Ingestor CRD is in the same namespace

---

apiVersion: adx-mon.azure.com/v1
kind: Alerter
metadata:
  name: minimal-alerter
spec:
  notificationEndpoint: "http://alertmanager.example.com:9093/api/v1/alerts"
  adxClusterSelector:
    matchLabels:
      app: my-adx # Selects the ADXCluster above
```

#### Complete (Fully Customized with Provisioning) Example

This example demonstrates provisioning a new ADX cluster and configuring all components with custom settings.

```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: prod-adx-cluster
  labels:
    env: production
    team: monitoring
spec:
  clusterName: adxmonprod01 # Desired name in Azure
  # No 'endpoint' means provision a new cluster
  provision:
    subscriptionId: "00000000-0000-0000-0000-000000000000"
    resourceGroup: "adx-monitor-prod-rg"
    location: "eastus2"
    skuName: "Standard_L8as_v3"
    tier: "Standard"
    # managedIdentityClientId: "11111111-1111-1111-1111-111111111111" # Optional: User-assigned MSI
  databases:
    - databaseName: "ProdMetrics"
      telemetryType: Metrics
      retentionInDays: 90
    - databaseName: "ProdLogs"
      telemetryType: Logs
      retentionInDays: 30

---

apiVersion: adx-mon.azure.com/v1
kind: Ingestor
metadata:
  name: prod-ingestor
spec:
  image: "myacr.azurecr.io/adx-mon/ingestor:v1.2.3"
  replicas: 3
  # endpoint: "ingestor-service.prod.svc.cluster.local" # Optional: Override auto-generated endpoint
  exposeExternally: false
  adxClusterSelector:
    matchLabels:
      env: production # Selects the ADXCluster above

---

apiVersion: adx-mon.azure.com/v1
kind: Collector
metadata:
  name: prod-collector
spec:
  image: "myacr.azurecr.io/adx-mon/collector:v1.2.3"
  # ingestorEndpoint: "http://ingestor-service.prod.svc.cluster.local:8080" # Optional: Override auto-discovery

---

apiVersion: adx-mon.azure.com/v1
kind: Alerter
metadata:
  name: prod-alerter
spec:
  image: "myacr.azurecr.io/adx-mon/alerter:v1.2.3"
  notificationEndpoint: "http://prod-alertmanager.example.com:9093/api/v1/alerts"
  adxClusterSelector:
    matchLabels:
      app: adx-mon
```

This configuration demonstrates:
- Full ADX cluster configuration with provisioning details
- Ingestor configuration with custom replica count, endpoint, and ADXCluster reference
- Collector configuration with explicit ingestor endpoint and custom image
- Alerter configuration with custom image, notification endpoint, and ADXCluster selector

---

## CRD Enhancements for Federated Cluster Support

To enable federated cluster functionality, the ADXCluster CRD is extended with new fields and sections as follows:

### Role Field

Specify the cluster's role:
- `role: Partition` (default, for data-holding clusters)
- `role: Federated` (for the central federating cluster)

### Federation Section

A new `federation` section encapsulates all federation-related configuration.

#### For Partition Clusters

```yaml
spec:
  role: Partition
  federation:
    federatedClusters:
      - endpoint: "https://federated.kusto.windows.net"
        heartbeatDatabase: "FleetDiscovery"
        heartbeatTable: "Heartbeats"
        managedIdentityClientId: "xxxx-xxxx-xxxx"
    partitioning:
      geo: "EU"
      location: "westeurope"
```
- `federatedClusters`: List of federated cluster endpoints and heartbeat config.
- `partitioning`: Open-ended map/object for partitioning metadata (geo, location, tenant, etc.).

#### For Federated Clusters

```yaml
spec:
  clusterName: hub
  endpoint: "https://hub.kusto.windows.net"
  provision:
    subscriptionId: "00000000-0000-0000-0000-000000000000"
    resourceGroup: "adx-monitor-prod"
    location: "eastus2"
    skuName: "Standard_L8as_v3"
    tier: "Standard"
    managedIdentityClientId: "11111111-1111-1111-1111-111111111111"
  role: Federated
  federation:
    heartbeatDatabase: "FleetDiscovery"
    heartbeatTable: "Heartbeats"
    heartbeatTTL: "1h"
    macroExpand:
      functionPrefix: "federated_"
      bestEffort: true
      folder: "federation_facades"
```
- `heartbeatDatabase`/`heartbeatTable`: Where to read heartbeats from.
- `heartbeatTTL`: How recent a heartbeat must be to consider a partition cluster live.
- `macroExpand`: Options for macro-expand KQL function generation.
  - `functionPrefix`: Prefix for generated KQL functions.
  - `bestEffort`: Use best effort mode for macro-expand.
  - `folder`: Folder in the federated cluster where macro-expand facades/functions are stored.

### Heartbeat Table Schema

The federated feature relies on a `heartbeat` table in the federated cluster to track the state and topology of partition clusters. The schema for this table is as follows:

| Column Name        | Type      | Description                                      |
|--------------------|-----------|--------------------------------------------------|
| Timestamp          | datetime  | When the heartbeat was sent                      |
| ClusterEndpoint    | string    | The endpoint of the partition cluster            |
| Schema             | dynamic   | Databases and tables present in the partition    |
| PartitionMetadata  | dynamic   | Partitioning attributes (geo, location, etc.)    |

#### Field Explanations
- **Timestamp**: The UTC time when the partition cluster emitted the heartbeat.
- **ClusterEndpoint**: The fully qualified endpoint URL of the partition cluster.
- **Schema**: A dynamic object describing the databases and tables present in the partition cluster. For example:
  ```json
  [
    {
      "database": "Logs",
      "tables": ["A", "B", "C"]
    }
  ]
  ```
- **PartitionMetadata**: A dynamic object containing the partitioning strategy and attributes for the cluster, such as geo or location. For example:
  ```json
  {
    "geo": "EU",
    "location": "westeurope"
  }
  ```

#### Example Log Row
```json
{
    "Timestamp": "2025-05-03T12:34:56Z",
    "ClusterEndpoint": "https://eu-partition.kusto.windows.net",
    "Schema": [
        {
            "database": "Logs",
            "tables": ["A", "B", "C"]
        }
    ],
    "PartitionMetadata": {
        "geo": "EU",
        "location": "westeurope"
    }
}
```

### Example CRD Snippets

**Partition Cluster Example:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: eu-partition
spec:
  role: Partition
  federation:
    federatedClusters:
      - endpoint: "https://federated.kusto.windows.net"
        heartbeatDatabase: "FleetDiscovery"
        heartbeatTable: "Heartbeats"
        managedIdentityClientId: "xxxx-xxxx"
    partitioning:
      geo: "EU"
      location: "westeurope"
  # ...existing cluster config...
```

**Federated Cluster Example:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: federated
spec:
  role: Federated
  federation:
    heartbeatDatabase: "FleetDiscovery"
    heartbeatTable: "Heartbeats"
    heartbeatTTL: "1h"
    macroExpand:
      functionPrefix: "federated_"
      bestEffort: true
      folder: "federation_facades"
  # ...existing cluster config...
```

These enhancements provide a clear, extensible way to configure both partition and federated clusters for the federation feature, including macro-expand facade management.

---

## Operator Workflow

1. **Granular Reconciliation:**  
   - The operator proceeds in incremental steps, updating subresource conditions for each phase:
     - Upsert Azure-managed ADX clusters.
     - Wait for ADX clusters to be ready.
     - Upsert ADX databases.
     - Wait for databases to be ready.
     - Create ingestor StatefulSet (parameterized with ADX connection strings).
     - Wait for ingestor to be ready.
     - Create collector DaemonSet (parameterized with ingestor endpoints).
     - Wait for collector to be ready.
     - Create alerter if specified.
   - Each step is only attempted after all dependencies are ready.

2. **Watch Managed Resources:**  
   - Monitor StatefulSets/Deployments for adx-mon components.
   - If a managed resource is changed outside the operator (e.g., replica count updated), the operator will reconcile the resource back to the value specified in the CRD. The CRD spec is always the source of truth for fields like replica count; the operator does not perform two-way sync. Status fields in the CRD are updated to reflect the current state and any reconciliation actions taken.

3. **Sync Desired and Actual State:**  
   - If the Operator CRD is updated, reconcile the manifests and Azure resources as needed.
   - If the actual state drifts from the desired state, update the actual state to match the desired state.

---

## Federated Cluster Support

### Overview

The operator supports a federated ADX cluster model to enable organizations to partition telemetry data across multiple Azure Data Explorer (ADX) clusters, often to meet geographic or regulatory data residency requirements. The federated model provides a "single pane of glass" experience for querying and analytics, allowing users to query a central federated cluster that transparently federates queries across all partition clusters.

### Architecture

- **Partition Clusters:**  
  Each partition cluster is managed by its own ADX operator instance and contains a subset of the data, typically partitioned by geography or other business criteria.

- **Federated Cluster:**  
  A central federated cluster is managed by a federated operator. This cluster does not manage the lifecycle of partition clusters, but provides a unified query interface.

### Discovery Mechanism

- **Heartbeat-Based Discovery:**  
  - Partition cluster operators are configured with the endpoint(s) of the federated cluster via the ADX CRD.
  - Each partition cluster operator periodically sends a heartbeat log row to a specified database and table in the federated cluster.
  - The heartbeat includes:
    - Partition cluster endpoint
    - List of databases and tables (as a dynamic object)
    - Partitioning schema (e.g., location, geo, tenant) as a dynamic object
    - Timestamp
  - The federated operator periodically queries the heartbeat table (e.g., `Heartbeats | where Timestamp > ago(1h) | distinct Endpoint`) to discover live partition clusters and their topologies.
  - Liveness is inferred from heartbeat freshness; clusters that have not heartbeated recently are excluded from federated queries.
  - Retention policy on the heartbeat table ensures cleanup of old/stale entries.

- **Authentication:**  
  - Partition clusters authenticate to the federated cluster using Microsoft-supported mechanisms (e.g., Managed Service Identity), with the identity specified in the CRD.

### Federated Querying

- **Macro-Expand Operator:**  
  - The federated cluster uses the Kusto macro-expand operator to define KQL functions or queries that union data from all live partition clusters.
  - Entity groups for macro-expand are dynamically constructed based on the current fleet topology, as discovered via heartbeats.
  - This enables the federated cluster to present a single logical view for each table, while queries are transparently federated across all relevant clusters and databases.
  - The federated operator updates KQL functions and entity groups as the fleet topology changes.

- **Schema Flexibility:**  
  - The dynamic object for database/table topology allows the federated operator to only federate queries to clusters that actually contain the relevant table, improving efficiency and flexibility.

### Operational Considerations

- Heartbeat interval and retention policy are configurable.
- MSI permissions must be correctly configured for secure heartbeat writes.
- The federated operator must handle schema differences and errors gracefully.
- Metrics and alerts should be implemented for missed heartbeats or authentication failures.

---

## Implementation Notes

- The operator should use controller-runtime's watches and informers to efficiently detect changes to both its own CRD and the managed resources.
- Azure resources are the source of truth for infrastructure; the operator should not attempt to reconcile drifted Azure resources directly.
- The operator should be idempotent and safe to reapply.
- Subresource conditions should be used to provide granular status and progress reporting.
- Future versions may support more advanced customization, validation, and upgrade strategies.

---

## Future Enhancements

### Ingestor Auto-Scaler
- Implement an auto-scaler for the Ingestor component that dynamically adjusts the number of replicas based on workload or custom metrics.
- Add an option in the Ingestor CRD to enable or configure auto-scaling behavior (e.g., min/max replicas, scaling thresholds).

### ADX Cluster Federation
- Support for ADX cluster federation, enabling partitioning of data across multiple ADX clusters.
- Introduce a central ADX cluster that can federate queries across all partitioned clusters, providing a single pane of glass for querying and analytics.
- Enhance CRDs and operator logic to manage federated cluster relationships and query routing.