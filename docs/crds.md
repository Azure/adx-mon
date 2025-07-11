# CRD Reference

This page summarizes all Custom Resource Definitions (CRDs) managed by adx-mon, with links to detailed documentation, example YAML, field explanations, and intended use cases for each type.

| CRD Kind      | Description                                 | Spec Fields Summary | Example Link |
|---------------|---------------------------------------------|---------------------|--------------|
| ADXCluster    | Azure Data Explorer cluster (provisioned or existing, supports federation/partitioning) | clusterName, endpoint, databases, provision, role, federation | [Operator Design](designs/operator.md#adxcluster-crd) |
| Ingestor      | Ingests telemetry from collectors, manages WAL, uploads to ADX | image, replicas, endpoint, exposeExternally, adxClusterSelector | [Operator Design](designs/operator.md#ingestor-crd) |
| Collector     | Collects metrics/logs/traces, forwards to Ingestor | image, ingestorEndpoint | [Operator Design](designs/operator.md#collector-crd) |
| Alerter       | Runs alert rules, sends notifications        | image, notificationEndpoint, adxClusterSelector | [Operator Design](designs/operator.md#alerter-crd) |
| SummaryRule   | Automates periodic KQL aggregations with async operation tracking, time window management, cluster label substitutions, and criteria-based execution | database, name, body, table, interval, criteria | [Summary Rules](designs/summary-rules.md#crd) |
| Function      | Defines KQL functions/views for ADX          | name, body, database, table, isView, parameters | [Schema ETL](designs/schema-etl.md#crd) |
| ManagementCommand | Declarative cluster management commands  | command, args, target, schedule | [Management Commands](designs/management-commands.md#crd) |

---

## ADXCluster
**Purpose:** Defines an Azure Data Explorer (ADX) cluster for adx-mon to use, either by provisioning a new cluster or connecting to an existing one. Supports federation and partitioning for multi-cluster topologies.

**Example:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: prod-adx-cluster
spec:
  clusterName: prod-metrics
  endpoint: "https://prod-metrics.kusto.windows.net" # Omit to provision a new cluster
  databases:
    - databaseName: Metrics
      telemetryType: Metrics
      retentionInDays: 90
    - databaseName: Logs
      telemetryType: Logs
      retentionInDays: 30
  provision:
    subscriptionId: "00000000-0000-0000-0000-000000000000"
    resourceGroup: "adx-monitor-prod"
    location: "eastus2"
    skuName: "Standard_L8as_v3"
    tier: "Standard"
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
```
**Key Fields:**
- `clusterName`: Name for the ADX cluster.
- `endpoint`: Existing ADX cluster URI (omit to provision new).
- `databases`: List of databases to create/use.
- `provision`: Azure provisioning details (required if not using `endpoint`).
- `role`: `Partition` (default) or `Federated` for multi-cluster.
- `federation`: Federation/partitioning config for multi-cluster.

**Intended Use:** Provision or connect to ADX clusters, including advanced federation/partitioning for geo-distributed or multi-tenant setups.

---

## Ingestor
**Purpose:** Deploys the Ingestor, which buffers and uploads telemetry to ADX. Supports scaling, endpoint customization, and cluster selection.

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
**Key Fields:**
- `image`: Container image for the ingestor.
- `replicas`: Number of replicas.
- `endpoint`: Service endpoint (auto-generated if omitted).
- `exposeExternally`: Whether to expose outside the cluster.
- `adxClusterSelector`: Label selector for target ADXCluster.

**Intended Use:** Buffer, batch, and upload telemetry from Collectors to ADX, with support for sharding and high availability.

---

## Collector
**Purpose:** Deploys the Collector, which gathers metrics, logs, and traces from the environment and forwards them to the Ingestor.

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
**Key Fields:**
- `image`: Container image for the collector.
- `ingestorEndpoint`: Endpoint for the Ingestor (auto-discovered if omitted).

**Intended Use:** Collect telemetry from Kubernetes nodes, scrape Prometheus endpoints, and forward to Ingestor.

---

## Alerter
**Purpose:** Deploys the Alerter, which runs alert rules (KQL queries) and sends notifications to external systems.

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
**Key Fields:**
- `image`: Container image for the alerter.
- `notificationEndpoint`: Where to send alert notifications.
- `adxClusterSelector`: Label selector for target ADXCluster.

**Intended Use:** Run scheduled KQL queries and send alerts to HTTP endpoints (e.g., Alertmanager, PagerDuty).

---

## SummaryRule
**Purpose:** Automates periodic KQL aggregations (rollups, downsampling, or data import) in ADX.

**Example:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: hourly-avg-metric
spec:
  database: Metrics
  name: HourlyAvg
  body: |
    SomeMetric
    | where Timestamp between (_startTime .. _endTime)
    | summarize avg(Value) by bin(Timestamp, 1h)
  table: SomeMetricHourlyAvg
  interval: 1h
```

**Environment-Specific Example with Cluster Labels:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: environment-specific-summary
spec:
  database: MyDB
  name: EnvSummary
  body: |
    MyTable
    | where Timestamp between (_startTime .. _endTime)
    | where Environment == "_environment"
    | where Region == "_region" 
    | summarize count() by bin(Timestamp, 1h)
  table: MySummaryTable
  interval: 1h
```

**Criteria-Based Example for Conditional Execution:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: region-specific-summary
spec:
  database: MyDB
  name: RegionalSummary
  body: |
    MyTable
    | where Timestamp between (_startTime .. _endTime)
    | summarize count() by bin(Timestamp, 1h)
  table: MySummaryTable
  interval: 1h
  criteria:
    region:
      - eastus
      - westus
    environment:
      - production
```

**Key Fields:**
- `database`: Target ADX database.
- `name`: Logical name for the rule.
- `body`: KQL query to run. **Must include `_startTime` and `_endTime` placeholders** for time range filtering. Can optionally use `<key>` placeholders for environment-specific values.
- `table`: Destination table for results.
- `interval`: How often to run the summary (e.g., `1h`).
- `criteria`: _(Optional)_ Key/value pairs used to determine when a summary rule can execute. If empty or omitted, the rule always executes. Keys and values are deployment-specific and configured on ingestor instances via `--cluster-labels`. For a rule to execute, any one of the criteria must match (OR logic). Matching is case-insensitive.

**Required Placeholders:**
- `_startTime`: Replaced with the start time of the current execution interval as `datetime(...)`.
- `_endTime`: Replaced with the end time of the current execution interval as `datetime(...)`. **Note**: `_endTime` is automatically adjusted by subtracting 1 tick (100ns) to ensure non-overlapping time windows while maintaining data completeness.

**Recommendations:**

**Configure ingestionDelay:** Highly recommended to set `ingestionDelay` in your SummaryRule spec to account for data ingestion delays. This ensures your summary rules process complete data rather than missing recently ingested data. See the [cookbook guide](cookbook.md#using-ingestion-delay-for-data-completeness) for recommended delay values and configuration examples.

**Use this recommended pattern for time range filtering in your KQL:**
- **Recommended**: `where Timestamp between (_startTime .. _endTime)` - Uses Kusto's inclusive `between` operator with automatic 1-tick (100ns) adjustment for non-overlapping windows

**Avoid this pattern:**
- **Not Recommended**: `where Timestamp >= _startTime and Timestamp < _endTime` - This pattern should be avoided as it will miss 100 nanoseconds of data due to the automatic adjustment applied to `_endTime`. Use the `between` syntax instead.

**Optional Cluster Label Substitutions:**
- `<key>`: Replaced with cluster-specific values defined by the ingestor's `--cluster-labels=<key>=<value>` command line arguments. Values are automatically quoted with single quotes for safe KQL usage.

**Intended Use:** Automate rollups, downsampling, or ETL in ADX by running scheduled KQL queries. Use cluster label substitutions to create environment-agnostic rules that work across different deployments.

### How SummaryRules Work Internally

SummaryRules are managed by the Ingestor's `SummaryRuleTask`, which runs periodically to:

1. **Time Window Management**: Calculate precise execution windows based on the last successful execution time and the rule's interval. First execution starts from current time minus one interval; subsequent executions continue from where the previous execution ended.

2. **Async Operation Lifecycle**: Submit KQL queries to ADX using `.set-or-append async <table> <| <query>` and track the resulting async operations through completion. Each operation gets an OperationId that's monitored until completion.

3. **State Tracking**: Uses Kubernetes conditions to track:
   - `SummaryRuleOwner`: Overall rule status (True/False/Unknown)
   - `SummaryRuleOperationIdOwner`: Stores up to 200 async operations as JSON in the condition message
   - `SummaryRuleLastSuccessfulExecution`: Tracks the end time of the last successful execution

4. **Operation Polling**: Regularly polls ADX's `.show operations` to check status of tracked async operations. Operations can be in states: `Completed`, `Failed`, `InProgress`, `Throttled`, or `Scheduled`.

5. **Resilience**: Handles ADX cluster restarts, network issues, and operation retries. Operations older than 25 hours are automatically cleaned up if they fall out of ADX's 24-hour operations window.

**Execution Triggers**: Rules are submitted when:
- Rule is being deleted
- Rule was updated (new generation)
- Time for next interval has elapsed

**Error Handling**: Uses `UpdateStatusWithKustoErrorParsing()` to extract meaningful error messages from ADX responses and truncate long errors to 256 characters.

---

## Function
**Purpose:** Defines a KQL function or view in ADX, allowing you to encapsulate reusable queries or present custom schemas for logs/metrics.

**Example:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: Function
metadata:
  name: custom-log-view
spec:
  name: CustomLogView
  body: |
    Logs
    | extend Level = tostring(Body.lvl), Message = tostring(Body.msg)
    | project Timestamp, Level, Message, Pod = Resource.pod
  database: Logs
  table: CustomLogView
  isView: true
  parameters: []
```
**Key Fields:**
- `name`: Name of the function/view in ADX.
- `body`: KQL body of the function.
- `database`: Target database.
- `table`: (Optional) Table the function is associated with (required for views).
- `isView`: If true, creates a view.
- `parameters`: List of parameters for the function.

**Intended Use:** Encapsulate reusable KQL logic or present custom schemas for easier querying.

---

## ManagementCommand
**Purpose:** Declaratively run management operations (e.g., optimize, update policy) on ADX clusters.

**Example:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ManagementCommand
metadata:
  name: optimize-table
spec:
  command: .optimize
  args:
    - Logs
    - kind=moveextents
  target: Logs
  schedule: "0 2 * * *"  # Run daily at 2am
```
**Key Fields:**
- `command`: The Kusto management command to run.
- `args`: Arguments for the command.
- `target`: Target table or database.
- `schedule`: Cron schedule for execution.

**Intended Use:** Automate cluster/table management tasks on a schedule.

---

- For full field documentation and advanced usage, see the linked sections above or the [Operator Design](designs/operator.md#crd-design) doc.
- Example YAML for each CRD is provided above and in the linked documentation.
- Federation and partitioning options are described in the [Federation section](designs/operator.md#crd-enhancements-for-federated-cluster-support).

---

Return to [Index](index.md) or [Concepts](concepts.md).