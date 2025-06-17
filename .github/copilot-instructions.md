Ensure all go code is formatted with `go fmt` (excluding vendor).
If any changes are made to the CRDs in `./api/v1`, be sure to generate the manifests via `make generate-crd CMD=update`
When any changes are made to `./build/k8s/`, ensure to run `make k8s-bundle` to regenerate the bundle.
CRD definitions are in `./kustomize/bases/` and `./operator/manifests/crds/`.
Ensure all code is tested with `go test ./...`.
