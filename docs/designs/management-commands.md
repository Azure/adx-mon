# Management Commands

## Background

We have internal tooling that performs various management commands against our Kusto clusters. We aim to retire this tooling by integrating these commands into adx-mon. Examples of such commands include [Workload Groups](https://learn.microsoft.com/en-us/kusto/management/workload-groups?view=microsoft-fabric) and [Request Classification Policies](https://learn.microsoft.com/en-us/kusto/management/request-classification-policy?view=microsoft-fabric).

Our existing _Functions_ CRD can handle such commands, but we plan to introduce validation to our _Functions_ to prevent arbitrary management commands. Therefore, we will not use _Functions_ for management commands.

## Goals

Enable the execution of arbitrary management commands for Kusto cluster configuration.

## Non-goals

Implementing security or RBAC for these commands is not within the scope of this initial design.

## Proposed Solution

We will define a new CRD, `ManagementCommands`, to support the execution of arbitrary management commands against a Kusto cluster. This CRD will ensure that we continually drive towards a desired state rather than executing commands as one-shot operations.

### CRD

**Cluster-Scoped Command:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ManagementCommand
metadata:
  name: some-request-classification
spec:
  scope: Cluster
  body: |
    .alter cluster policy request_classification '{"IsEnabled":true}' <|
        case(current_principal_is_member_of('aadgroup=somesecuritygroup@contoso.com'), "First workload group",
            request_properties.current_database == "MyDatabase" and request_properties.current_principal has 'aadapp=', "Second workload group",
            request_properties.current_application == "Kusto.Explorer" and request_properties.request_type == "Query", "Third workload group",
            request_properties.current_application == "KustoQueryRunner", "Fourth workload group",
            request_properties.request_description == "this is a test", "Fifth workload group",
            hourofday(now()) between (17 .. 23), "Sixth workload group",
            "default")
```

**Database-Scoped Command:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ManagementCommand
metadata:
  name: clear-database-cache
spec:
  database: MyDatabase
  scope: Database
  body: .clear database cache query_results
```

**All Databases Command (e.g., table creation across all databases):**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: ManagementCommand
metadata:
  name: create-audit-table-all-dbs
spec:
  scope: AllDatabases
  body: .create table AuditLog (Timestamp:datetime, Action:string, User:string)
```

### Scope Field

The `scope` field explicitly defines the execution scope of the management command:

| Scope Value | Description |
|-------------|-------------|
| `Database` | Command targets the specific database in the `database` field. Requires `database` to be set. |
| `AllDatabases` | Command is executed against each database that the ingestor serves. Useful for table creation, policy application, etc. |
| `Cluster` | Command is cluster-scoped (e.g., cluster policies, workload groups). |

### Backwards Compatibility

For backwards compatibility:
- If `database` is non-empty, it takes precedence over `scope` (command is database-scoped).
- If both `database` and `scope` are empty, the command defaults to cluster-scoped (deprecated behavior).

**Deprecated patterns:**
- Leaving `database` empty without setting `scope` for cluster-scoped commands. Use `scope: Cluster` instead.

> **See also:** [CRD Reference](../crds.md) for a summary of all CRDs and links to advanced usage.