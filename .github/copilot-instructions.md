Ensure all go code is formatted with `go fmt` (excluding vendor).
Kubernetes manifests are in `./operator/manifests/` and `./build/k8s/`.
When any changes are made to `./build/k8s/`, ensure to run `make k8s-bundle` to regenerate the bundle.
CRD definitions are in `./kustomize/bases/` and `./operator/manifests/crds/`.
Ensure all code is tested with `go test ./...`.
