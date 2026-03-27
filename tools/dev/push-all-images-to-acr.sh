#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "Usage: $0 <acr-name-or-login-server> [tag]" >&2
  exit 1
fi

ACR_INPUT="$1"
TAG="${2:-latest}"

"${SCRIPT_DIR}/push-ingestor-to-acr.sh" "${ACR_INPUT}" "${TAG}"
"${SCRIPT_DIR}/push-collector-to-acr.sh" "${ACR_INPUT}" "${TAG}"
"${SCRIPT_DIR}/push-operator-to-acr.sh" "${ACR_INPUT}" "${TAG}"
"${SCRIPT_DIR}/push-alerter-to-acr.sh" "${ACR_INPUT}" "${TAG}"
