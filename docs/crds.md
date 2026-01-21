# CRD Reference

This page summarizes all Custom Resource Definitions (CRDs) managed by adx-mon, with links to detailed documentation, example YAML, field explanations, and intended use cases for each type.

| CRD Kind      | Description                                 | Spec Fields Summary | Example Link |
|---------------|---------------------------------------------|---------------------|--------------|
| ADXCluster    | Azure Data Explorer cluster (provisioned or existing, supports federation/partitioning) | clusterName, endpoint, databases, provision, role, federation | [ADXCluster Controller](adxcluster-controller.md) |
| Ingestor      | Ingests telemetry from collectors, manages WAL, uploads to ADX | image, replicas, endpoint, exposeExternally, adxClusterSelector | [Operator Design](designs/operator.md#ingestor-crd) |
| Collector     | Collects metrics/logs/traces, forwards to Ingestor | image, ingestorEndpoint | [Operator Design](designs/operator.md#collector-crd) |
| Alerter       | Runs alert rules, sends notifications        | image, notificationEndpoint, adxClusterSelector | [Operator Design](designs/operator.md#alerter-crd) |
| SummaryRule   | Automates periodic KQL aggregations with async operation tracking, time window management, cluster label substitutions, and criteria-based execution | database, name, body, table, interval, criteria | [Summary Rules](designs/summary-rules.md#crd) |
| MetricsExporter | Executes KQL queries and exports results as metrics to OTLP endpoints | database, body, interval, transform, criteria | [Kusto-to-Metrics](designs/kusto-to-metrics.md) |
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

**Federated hub example with blocked clusters (static + dynamic):**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ADXCluster
metadata:
  name: global-hub
spec:
  clusterName: global-hub
  endpoint: "https://global-hub.kusto.windows.net"
  role: Federated
  federation:
    heartbeatDatabase: "FleetDiscovery"
    heartbeatTable: "Heartbeats"
    heartbeatTTL: "2h"
    blockedClusters:
      static:
        - "https://legacy-partition.kusto.windows.net"
        - "https://quarantined-partition.kusto.windows.net/"  # trailing slashes ignored
      kustoFunction:
        database: FleetDiscovery
        name: GetBlockedPartitions
```
The controller normalizes each endpoint (lower case and no trailing slash) and merges the static list with the results of
`GetBlockedPartitions()` before generating macros. Any partition whose heartbeat endpoint matches the merged list is
excluded from schema fan-out.

**Key Fields:**
- `clusterName`: Name for the ADX cluster.
- `endpoint`: Existing ADX cluster URI (omit to provision new). When set, the controller mirrors this value into status
  and skips Azure provisioning.
- `databases`: List of databases to create/use. The controller only provisions the entries listed here (plus the
  heartbeat database when federation is enabled); it no longer injects default databases.
- `provision`: Azure provisioning details (required if not using `endpoint`). When set, supply `subscriptionId`,
  `resourceGroup`, `location`, `skuName`, and `tier` explicitly—the controller no longer auto-detects or mutates these
  values.
- `role`: `Partition` (default) or `Federated` for multi-cluster.
- `federation`: Federation/partitioning config for multi-cluster. Federated hubs can optionally specify
  `federation.blockedClusters` to exclude rogue or quarantined partitions from macro generation. The block list accepts a
  static array of endpoints as well as a break-glass Kusto function (returning `ClusterEndpoint` or `Endpoint` columns)
  whose results are normalized (trimmed, case-insensitive, trailing slashes removed) before filtering the partition
  schema set.

### Creating a blocked cluster function in Kusto
The optional `blockedClusters.kustoFunction` expects a function that returns a table containing either a
`ClusterEndpoint` or `Endpoint` string column. You can create one in the heartbeat database with a command like:

```kusto
.create-or-alter function with (docstring="Return partitions blocked from federation", folder="FleetSafety") GetBlockedPartitions()
{
  let manualOverrides = datatable(ClusterEndpoint:string)
  [
    "https://legacy-partition.kusto.windows.net",
    "https://maintenance-partition.kusto.windows.net"
  ];
  FleetSafetyBlockedPartitions
  | project ClusterEndpoint = tostring(ClusterEndpoint)
  | union manualOverrides
  | distinct ClusterEndpoint
}
```

- `FleetSafetyBlockedPartitions` can be any table you manage (for example, populated via Azure Monitor alerts or manual entries).
- The function may also emit an `Endpoint` column; the controller checks both and trims whitespace before use.
- Missing functions are logged as warnings, while other Kusto errors block reconciliation so operators can investigate.

**Status highlights:**
- `status.endpoint`: Observed Kusto endpoint used by dependent components (mirrors `spec.endpoint` in BYO mode).
- `status.appliedProvisionState`: Snapshot of the SKU, tier, and identities last reconciled in Azure. This lets the operator detect when a user changed the spec, ignore out-of-band Azure edits that would otherwise cause thrash, and surface the live provisioning settings to other controllers.

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

**Criteria / CriteriaExpression Based Conditional Execution:**
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
  # Optional CEL expression – all cluster labels are exposed as lower-cased string variables
  criteriaExpression: |
    (region in ['eastus','westus']) && environment == 'production'
```

**Key Fields:**
- `database`: Target ADX database.
- `name`: Logical name for the rule.
- `body`: KQL query to run. **Must include `_startTime` and `_endTime` placeholders** for time range filtering. Can optionally reference cluster label variables using underscore-prefixed names (e.g., `_environment`, `_region`).
- `table`: Destination table for results.
- `interval`: How often to run the summary (e.g., `1h`).
- `criteria`: _(Optional)_ Key/value pairs used to determine when a summary rule can execute. If empty or omitted, the rule always executes. Keys and values are deployment-specific and configured on ingestor instances via `--cluster-labels`. For a rule to execute, any one of the criteria must match (OR logic). Matching is case-insensitive.
- `criteriaExpression`: _(Optional)_ A CEL expression evaluated against the ingestor's cluster labels (all label keys are lower‑cased and provided as string variables). Combined semantics: `(criteria empty OR any criteria pair matches) AND (criteriaExpression empty OR expression evaluates to true)`. Invalid expressions (parse/type/eval error) result in the rule being skipped.

**Required Placeholders:**
- `_startTime`: Replaced with the start time of the current execution interval as `datetime(...)`.
- `_endTime`: Replaced with the end time of the current execution interval as `datetime(...)`.

**Optional Cluster Label Variables:**
Cluster labels defined via `--cluster-labels=<key>=<value>` are exposed as KQL `let` statement variables with underscore prefixes. For example, `--cluster-labels=region=eastus` creates `let _region="eastus";` prepended to the query body. Values are automatically double-quoted for safe KQL usage. These variables can be referenced directly in your KQL queries.

**Intended Use:** Automate rollups, downsampling, or ETL in ADX by running scheduled KQL queries. Use cluster label substitutions to create environment-agnostic rules that work across different deployments.

### How SummaryRules Work Internally

#### Shared Execution Selection Logic (criteria / criteriaExpression)
All rule-like CRDs (AlertRule, SummaryRule, MetricsExporter) now share a unified evaluation path:

1. Normalize cluster label keys to lower case and expose them as CEL variables (string values also lower-cased for value comparison convenience).
2. Evaluate legacy `criteria` map with OR semantics (case-insensitive key + value matches).
3. If a `criteriaExpression` is present, compile and evaluate it with [CEL](https://cel.dev/).
4. Execute only if both the map check and the expression pass (empty pieces are permissive).

Examples:
```yaml
# Map only
criteria:
  region: [eastus, westus]

# Expression only
criteriaExpression: region in ['eastus','westus'] && environment == 'prod'

# Both (AND semantics)
criteria:
  region: [eastus]
criteriaExpression: environment == 'prod' && cloud == 'public'
```

This logic is implemented once in `pkg/celutil` and reused by Alerter, Ingestor (SummaryRules), and Metrics Exporter for consistency.

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

## MetricsExporter
**Purpose:** Executes KQL queries against Azure Data Explorer and exports the results as metrics to OTLP-compatible endpoints. Unlike SummaryRules which store results in ADX tables, MetricsExporter transforms query results into metrics format and pushes them directly to observability platforms.

**Example:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: service-response-times
  namespace: monitoring
spec:
  database: TelemetryDB
  interval: 5m
  criteria:
    region: ["eastus", "westus"]
    environment: ["production"]
  body: |
    ServiceTelemetry
    | where Timestamp between (_startTime .. _endTime)
    | summarize 
        metric_value = avg(ResponseTimeMs),
        timestamp = bin(Timestamp, 1m)
        by ServiceName, Environment
    | extend metric_name = "service_response_time_avg"
  transform:
    metricNameColumn: "metric_name"
    valueColumns: ["metric_value"]
    timestampColumn: "timestamp"
    labelColumns: ["ServiceName", "Environment"]
```

**Key Fields:**
- `database`: Target ADX database to query.
- `body`: KQL query to execute. **Must include `_startTime` and `_endTime` placeholders** for time range filtering.
- `interval`: How often to execute the query and export metrics (e.g., `5m`, `1h`).
- `transform`: Configuration for transforming KQL results to metrics:
  - `metricNameColumn`: Column containing metric names (optional if `defaultMetricName` is set).
  - `valueColumns`: Array of columns containing numeric metric values.
  - `timestampColumn`: Column containing timestamps for the metrics.
  - `labelColumns`: Columns to use as metric labels/attributes.
  - `defaultMetricName`: Fallback metric name if `metricNameColumn` is not specified.
  - `metricNamePrefix`: Optional prefix to prepend to all metric names.
- `criteria`: _(Optional)_ Key/value pairs for criteria-based execution selection (same as SummaryRule).
- `criteriaExpression`: _(Optional)_ CEL expression for advanced criteria matching.

**Required Placeholders:**
- `_startTime`: Replaced with the start time of the execution window.
- `_endTime`: Replaced with the end time of the execution window.

**adxexporter CLI Configuration:**
```bash
adxexporter \
  --cluster-labels="region=eastus,environment=production" \
  --kusto-endpoint="TelemetryDB=https://cluster.kusto.windows.net" \
  --otlp-endpoint="http://otel-collector:4318/v1/metrics" \
  --metric-name-prefix="adxexporter"
```

**CLI Parameters:**
- `--cluster-labels`: Comma-separated key=value pairs for criteria matching.
- `--kusto-endpoint`: ADX endpoint in format `<database>=<endpoint>`. Can specify multiple.
- `--otlp-endpoint`: **(Required)** OTLP HTTP endpoint for pushing metrics.
- `--add-resource-attributes`: Key/value pairs of resource attributes to add to all exported metrics. Format: `<key>=<value>`. These are merged with cluster-labels (explicit attributes take precedence).
- `--metric-name-prefix`: Global prefix prepended to all metric names. Combined with CRD's `metricNamePrefix` (CLI prefix comes first). Useful for enforcing naming conventions or allow-list compliance.
- `--health-probe-port`: Port for health endpoints (default: 8081).

**Metric Naming:**

The final metric name is constructed as: `<CLI_prefix>_<CRD_prefix>_<metricName>_<valueColumn>`

The prefix is built by combining:
1. **CLI's `--metric-name-prefix`** — always prepended first (if set)
2. **CRD's `metricNamePrefix`** — appended after CLI prefix (if set)

For example, with `--metric-name-prefix=adxexporter` and a CRD with `metricNamePrefix: infra`:
- Metric name `host_count` with value column `count` → `adxexporter_infra_host_count_count`

This ensures operators can enforce a global prefix (e.g., for allow-list compliance) while teams can still add their own sub-prefixes via CRD.

**How It Works:**
1. **CRD Discovery**: `adxexporter` watches MetricsExporter CRDs matching its cluster labels.
2. **Query Execution**: Executes KQL queries on schedule with `_startTime`/`_endTime` substitution.
3. **Transform**: Converts query results to `MetricData` using the transform configuration.
4. **OTLP Push**: Converts metrics to `prompb.WriteRequest` format and pushes to the OTLP endpoint.

**Key Benefits:**
- **No Intermediate Storage**: Query results are transformed and exported directly without ADX table materialization.
- **Timestamp Fidelity**: Preserves actual query result timestamps (unlike Prometheus scraping).
- **Memory Efficient**: Uses `pkg/prompb` object pooling for high cardinality metrics.
- **Criteria-Based Execution**: Same security and distribution model as SummaryRules.

**Intended Use:** Export ADX analytics data as standardized metrics to external observability platforms (Prometheus, Grafana, DataDog, etc.) without creating intermediate tables.

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
  body: |
    .create-or-alter function with (view=true, folder='views') CustomLogView () {
      Logs
      | extend Level = tostring(Body.lvl), Message = tostring(Body.msg)
      | project Timestamp, Level, Message, Pod = Resource.pod
    }
  database: Logs
```

**Key Fields:**
- `name`: Name of the function/view in ADX.
- `body`: KQL body of the function.
- `database`: Target database.
- `table`: (Optional) Table the function is associated with (required for views).
- `isView`: If true, creates a view.
- `parameters`: List of parameters for the function.

**Intended Use:** Encapsulate reusable KQL logic or present custom schemas for easier querying.

### Status Conditions

`Function` resources surface detailed reconciliation state via Kubernetes conditions. These appear in `kubectl describe function <name>` and provide actionable diagnostics without digging into ingestor logs. Functions whose `spec.database` does not match the ingestor's configured database are skipped silently; the controller leaves the existing conditions untouched to avoid flapping status between ingestors.

| Condition Type | Meaning | Typical Status / Reasons |
|----------------|---------|---------------------------|
| `function.adx-mon.azure.com/CriteriaMatch` | Result of evaluating `spec.criteriaExpression` against cluster labels. | `True` with reason `CriteriaMatched`; `False` with reason `CriteriaNotMatched` (expression evaluated to false) or `CriteriaExpressionError` (parse/evaluation failure). |
| `function.adx-mon.azure.com/Reconciled` | Outcome of the most recent reconciliation attempt. | `True` with reason `KustoExecutionSucceeded` or `FunctionDeleted`; `False` for intermediate states such as `CriteriaNotMatched`, `CriteriaEvaluationFailed`, `KustoExecutionRetrying`, or `KustoExecutionFailed`. |

Example describe output when a criteria mismatch occurs:

```yaml
Status:
  Conditions:
  - lastTransitionTime: "2025-10-16T20:05:00Z"
    message: Function skipped because criteria expression evaluated to false for cluster labels: environment=prod, location=westus2
    observedGeneration: 2
    reason: CriteriaNotMatched
    status: "False"
    type: function.adx-mon.azure.com/Reconciled
  - lastTransitionTime: "2025-10-16T20:05:00Z"
    message: Criteria expression evaluated to false for cluster labels: environment=prod, location=westus2
    observedGeneration: 2
    reason: CriteriaNotMatched
    status: "False"
    type: function.adx-mon.azure.com/CriteriaMatch
```

Controllers always set `observedGeneration` to the processed spec generation, making it easy to confirm whether the latest edit has been reconciled. Messages are capped at 256 characters (consistent with other CRDs) to avoid log spam while remaining human-readable.

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