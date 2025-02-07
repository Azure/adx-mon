# ADX-Mon


ADX-Mon is a comprehensive observability platform that seamlessly integrates metrics, logs, traces, continuous profile
and any telemetry into a unified platform. It addresses the traditional challenges of data being siloed and difficult
to correlate, as well as the cardinality and scale issues found in existing metrics solutions, streamlining the
collection and analysis of observability data.

## Features

* **Unlimited Cardinality, Retention, and Granularity**: No restrictions on data dimensions, storage duration, or detail level.
* **Low Latency Ingestion and Lightweight Collection**: Ensures rapid data processing with minimal resource overhead.
* **Unified Alerting**: Simplified alerting mechanisms for metrics, logs, and traces.
* **Powered by Azure Data Explorer (ADX)**: Built on the robust and scalable [ADX](https://azure.microsoft.com/en-us/products/data-explorer) platform with the powerful [Kusto Query Language (KQL)](https://learn.microsoft.com/en-us/azure/data-explorer/kusto/query/) unifying access to all telemetry.
* **Flexible and Standards-Based Interfaces**: Supports OTEL, Prometheus, and other industry standards ingestion protocols.
* **Optimized for Kubernetes and Cloud-Native Environments**: Designed to thrive in modern, dynamic infrastructures.
* **Pluggable Alerting Provider API**: Customizable alerting integrations.
* **Broad Compatibility**: Works seamlessly with Grafana, ADX Dashboards, PowerBI, and any ADX-compatible products.
* **Turnkey, Scalable, and Reliable**: A ready-to-use observability platform that scales from hobby projects to enterprise-level deployments.

## Learn More

* [Getting Started](https://azure.github.io/adx-mon/quick-start/)
* [Concepts](https://azure.github.io/adx-mon/concepts/)
* [Cook Book](https://azure.github.io/adx-mon/cookbook/)


## Development

### CRDs

[kubebuilder](https://book.kubebuilder.io/) is used to generate adx-mon's CRDs. 

* Create a new CRD: `kubebuilder create api --group adx-mon --version v1 --kind CRD_NAME --resource --make --controller=false`, replacing `CRD_NAME` with the name of the CRD you want to create.
* To update a CRD, modify the appropriate spec-type in the _api_ directory and then run `make manifests` to update the CRD specs in `kustomize/bases`

## Contributing

This project is currently under heavy development and is changing frequently.  _We are currently only
accepting contributions for bug fixes, test coverage and documentation_.  Feature requests will be
considered after the project has reached a stable state.

Most contributions require you to agree to a Contributor License Agreement (CLA) declaring that you have the right to,
and actually do, grant us the rights to use your contribution. For details, visit https://cla.opensource.microsoft.com.

When you submit a pull request, a CLA bot will automatically determine whether you need to provide
a CLA and decorate the PR appropriately (e.g., status check, comment). Simply follow the instructions
provided by the bot. You will only need to do this once across all repos using our CLA.

This project has adopted the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/).
For more information see the [Code of Conduct FAQ](https://opensource.microsoft.com/codeofconduct/faq/) or
contact [opencode@microsoft.com](mailto:opencode@microsoft.com) with any additional questions or comments.

## Trademarks

This project may contain trademarks or logos for projects, products, or services. Authorized use of Microsoft 
trademarks or logos is subject to and must follow 
[Microsoft's Trademark & Brand Guidelines](https://www.microsoft.com/en-us/legal/intellectualproperty/trademarks/usage/general).
Use of Microsoft trademarks or logos in modified versions of this project must not cause confusion or imply Microsoft sponsorship.
Any use of third-party trademarks or logos are subject to those third-party's policies.


## Testing alertrules
Prereq: some k8s cluster you have kubeconfig for (can be minikube or kind)

1. az login to the account that has acces to the kusto clusters. 
1. apply crds :  k apply -f kustomize/bases/alertrules_crd.yaml 
1. apply your alert rules (probably in a different repo)
1. cd cmd/alerter
1. go run . --kusto-endpoint "<database>=<kustourl>"  --kubeconfig ~/.kube/config. The database much match the one in your alert rule (case-sensitive)
1. optionally specicfy arugments that will be filled into query if needed like --region uksouth
1. You should both see  "Creating query executor for..." and "Fake Alert Notification Recieved..." if your query gets results.
