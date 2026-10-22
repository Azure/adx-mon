#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/acr-image-common.sh"

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "Usage: $0 <acr-name-or-login-server> [tag]" >&2
  exit 1
fi

require_basics

ACR_INPUT="$1"
TAG="${2:-latest}"
LOGIN_SERVER="$(resolve_acr_login_server "${ACR_INPUT}")"
ACR_NAME="$(acr_name_from_login_server "${LOGIN_SERVER}")"

az acr login --name "${ACR_NAME}" >/dev/null

IMAGE_REF="${LOGIN_SERVER}/adx-mon/operator:${TAG}"
docker_build_and_push "${REPO_ROOT}/build/images/Dockerfile.operator" "${IMAGE_REF}"

echo "Pushed ${IMAGE_REF}"
