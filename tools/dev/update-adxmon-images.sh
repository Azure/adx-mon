#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "Usage: $0 <acr-name-or-login-server> [tag]" >&2
  exit 1
fi

if ! command -v kubectl >/dev/null 2>&1; then
  echo "Missing required command: kubectl" >&2
  exit 1
fi

ACR_INPUT="$1"
TAG="${2:-latest}"
NAMESPACE="${NAMESPACE:-adx-mon}"

if [[ "${ACR_INPUT}" == *.azurecr.io ]]; then
  LOGIN_SERVER="${ACR_INPUT}"
else
  LOGIN_SERVER="${ACR_INPUT}.azurecr.io"
fi

INGESTOR_IMAGE="${LOGIN_SERVER}/adx-mon/ingestor:${TAG}"
COLLECTOR_IMAGE="${LOGIN_SERVER}/adx-mon/collector:${TAG}"

set_pull_policy_always() {
  local resource="$1"
  local container="$2"
  kubectl -n "${NAMESPACE}" patch "${resource}" --type='strategic' -p "{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"${container}\",\"imagePullPolicy\":\"Always\"}]}}}}"
}

kubectl -n "${NAMESPACE}" set image statefulset/ingestor ingestor="${INGESTOR_IMAGE}"
kubectl -n "${NAMESPACE}" set image daemonset/collector collector="${COLLECTOR_IMAGE}"
kubectl -n "${NAMESPACE}" set image deployment/collector-singleton collector="${COLLECTOR_IMAGE}"

set_pull_policy_always statefulset/ingestor ingestor
set_pull_policy_always daemonset/collector collector
set_pull_policy_always deployment/collector-singleton collector

kubectl -n "${NAMESPACE}" rollout status statefulset/ingestor
kubectl -n "${NAMESPACE}" rollout status daemonset/collector
kubectl -n "${NAMESPACE}" rollout status deployment/collector-singleton

echo "Updated workloads in namespace ${NAMESPACE}"
echo "- statefulset/ingestor -> ${INGESTOR_IMAGE}"
echo "- daemonset/collector -> ${COLLECTOR_IMAGE}"
echo "- deployment/collector-singleton -> ${COLLECTOR_IMAGE}"
