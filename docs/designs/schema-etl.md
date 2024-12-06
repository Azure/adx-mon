# Custom Schema ETL

## Summary

Logs are ingested into Tables using [OTLP schema](https://opentelemetry.io/docs/specs/otel/logs/data-model/#log-and-event-record-definition). There are several limitations in querying OTLP directly that can be solved by supporting custom schema definitions.

## Solutions

Below we present two ways of defining a schema and two ways of implementing those schemas.

### Schema Definition

- CRD that specifies a Table's schema
- CRD that generically specifies KQL functions

### ETL

- Update Policies
Leveraging the [Medallion architecture](https://learn.microsoft.com/en-us/azure/databricks/lakehouse/medallion) logs are ingested into preliminary Tables using the OTLP schema definition and [update-policies](https://learn.microsoft.com/en-us/azure/data-explorer/open-telemetry-connector?tabs=command-line) are leveraged to perform ETL operations against the preliminary Table that then emit the logs with custom schemas applied before being stored in their final destinations. We have a great deal of experience with update-policies and have found several limitations, such as failed ingestions due to schema missalignment and increased runtime resource expenses. We have therefore decided to try another route.

- Views
A [View](https://learn.microsoft.com/en-us/kusto/query/schema-entities/views?view=microsoft-fabric) with the same name as a Table is defined, where a custom schema is defined and realized at query time. This means the content is stored as OTLP but able to be queried with a user defined schema. There will be a query-time performance penalty with this approach that we need to measure and determine if it's acceptable. 

### Proposed Solution

We will define a CRD that enables a user to define a KQL Function, with an optional parameter to specify the Function as being a View, whereby ETL operations within the View will present the user with their desired schema at the time of query.

## CRD

Our CRD could simply enable a user to specify any arbitrary KQL; however, to prevent admin commands from being executed, we'll instead specify all the possible fields for a Function and construct the KQL scaffolding ourselves.

The CRD definition is as follows:

```yaml
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.16.1
  name: functions.adx-mon.azure.com
spec:
  group: adx-mon.azure.com
  names:
    kind: Function
    listKind: FunctionList
    plural: functions
    singular: function
  scope: Namespaced
  versions:
  - name: v1
    schema:
      openAPIV3Schema:
        description: Function defines a KQL function to be maintained in the Kusto
          cluster
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            description: FunctionSpec defines the desired state of Function
            properties:
              body:
                description: Body is the KQL body of the function
                type: string
              database:
                description: Database is the name of the database in which the function
                  will be created
                type: string
              suspend:
                description: |-
                  This flag tells the controller to suspend subsequent executions, it does
                  not apply to already started executions.  Defaults to false.
                type: boolean
            required:
            - body
            - database
            type: object
          status:
            description: FunctionStatus defines the observed state of Function
            properties:
              error:
                description: Error is a string that communicates any error message
                  if one exists
                type: string
              lastTimeReconciled:
                description: LastTimeReconciled is the last time the Function was
                  reconciled
                format: date-time
                type: string
              message:
                description: Message is a human-readable message indicating details
                  about the Function
                type: string
              observedGeneration:
                description: ObservedGeneration is the most recent generation observed
                  for this Function
                format: int64
                type: integer
              status:
                description: Status is an enum that represents the status of the Function
                type: string
            required:
            - lastTimeReconciled
            - observedGeneration
            - status
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
```

A sample use is:

```yaml
apiVersion: adx-mon.azure.com/v1
kind: Function
metadata:
  name: samplefn
spec:
  name: SampleFn
  body: |
    SampleFn
    | extend Timestamp = todatetime(Body['ts']),
            Message = tostring(Body['msg']),
            Labels = Attributes['labels'],
            Host = tostring(Resource['Host'])
  database: SomeDatabase
  table: SampleFn
  isView: true
```

_Ingestor_ would then execute the following 

```kql
.create-or-alter function with ( view=true ) SampleFn () {
    SampleFn
    | extend Timestamp = todatetime(Body['ts']),
            Message = tostring(Body['msg']),
            Labels = Attributes['labels'],
            Host = tostring(Resource['Host'])
}
```

## Implementation Details

- Ingestor already manages the creation of Tables as logs flow through the system, so we'll have Ingestor also manage creation of these Functions because the Table must exist prior to the Function that it references.
- Alerter already handles CRDs, as much as possible we'll share code between these components so that we're not duplicating a bunch of code.
- When attempting to reconcile each Function's state, we'll make sure the process is idempotent such that if a Function definition already exists that matches all the parameters found in its CRD, we do not attempt to update its state.

## Questions
- If Ingestor has a reference to a CRD / Function that is later deleted, do we want to delete the associated Function in Kusto? 