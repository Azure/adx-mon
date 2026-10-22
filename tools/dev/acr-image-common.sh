#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

require_cmd() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "Missing required command: ${cmd}" >&2
    exit 1
  fi
}

require_basics() {
  require_cmd docker
  require_cmd az
}

resolve_acr_login_server() {
  local input="$1"

  if [[ -z "${input}" ]]; then
    echo "ACR name/login-server cannot be empty" >&2
    exit 1
  fi

  if [[ "${input}" == *.azurecr.io ]]; then
    printf "%s" "${input}"
    return
  fi

  az acr show --name "${input}" --query loginServer -o tsv
}

acr_name_from_login_server() {
  local login_server="$1"
  printf "%s" "${login_server%%.azurecr.io}"
}

compute_build_vars() {
  VERSION="${VERSION:-${TAG:-latest}}"
  GIT_COMMIT="${GIT_COMMIT:-$(git -C "${REPO_ROOT}" rev-parse --short HEAD)}"
  BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
}

docker_build_and_push() {
  local dockerfile="$1"
  local image_ref="$2"

  compute_build_vars

  docker build \
    --build-arg VERSION="${VERSION}" \
    --build-arg GIT_COMMIT="${GIT_COMMIT}" \
    --build-arg BUILD_TIME="${BUILD_TIME}" \
    -f "${dockerfile}" \
    -t "${image_ref}" \
    "${REPO_ROOT}"

  docker push "${image_ref}"
}
