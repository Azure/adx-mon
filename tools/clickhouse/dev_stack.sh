#!/usr/bin/env bash
set -Eeuo pipefail

command=${1:-help}
shift || true

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
STACK_DIR="${SCRIPT_DIR}/.stack"
CONFIG_DIR="${STACK_DIR}/collector"
COLLECTOR_CONFIG="${CONFIG_DIR}/collector.toml"
COLLECTOR_DATA="${STACK_DIR}/collector-data"
INGESTOR_DATA="${STACK_DIR}/ingestor-data"
CLICKHOUSE_DATA="${STACK_DIR}/clickhouse-data"
CONFIG_MOUNT="/etc/adx-mon/collector.toml"

NETWORK_NAME="adxmon-clickhouse-net"
CLICKHOUSE_CONTAINER="adxmon-clickhouse"
INGESTOR_CONTAINER="adxmon-ingestor"
COLLECTOR_CONTAINER="adxmon-collector"

COLLECTOR_IMAGE="adxmon/collector:clickhouse-dev"
INGESTOR_IMAGE="adxmon/ingestor:clickhouse-dev"
CLICKHOUSE_IMAGE="${CLICKHOUSE_IMAGE:-clickhouse/clickhouse-server:24.8}"
CLICKHOUSE_DB="${CLICKHOUSE_DB:-observability}"
LOGS_DB="${LOGS_DB:-observability_logs}"

BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
GIT_COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo "dev")"
VERSION_TAG="${VERSION_TAG:-dev}"  # override to stamp images

log() {
  printf '\n[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"
}

err() {
  printf '\n[ERROR] %s\n' "$*" >&2
}

require_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    err "docker CLI not found in PATH"
    exit 1
  fi
}

prepare_dirs() {
  mkdir -p "${CONFIG_DIR}" "${COLLECTOR_DATA}" "${INGESTOR_DATA}" "${CLICKHOUSE_DATA}"
}

write_collector_config() {
  if [[ -f "${COLLECTOR_CONFIG}" && "${FORCE_REGENERATE_CONFIG:-0}" != "1" ]]; then
    return
  fi

  cat >"${COLLECTOR_CONFIG}" <<EOF
endpoint = "https://ingestor:9090"
insecure-skip-verify = true
listen-addr = ":8080"
region = "dev"
storage-backend = "clickhouse"
storage-dir = "/var/lib/adx-mon/collector"
wal-flush-interval-ms = 100
max-batch-size = 5000

[[prometheus-remote-write]]
database = "${CLICKHOUSE_DB}"
path = "/receive"

[[otel-metric]]
database = "${CLICKHOUSE_DB}"
path = "/v1/metrics"
grpc-port = 4317
EOF
}

create_network() {
  if ! docker network inspect "${NETWORK_NAME}" >/dev/null 2>&1; then
    log "Creating network ${NETWORK_NAME}"
    docker network create "${NETWORK_NAME}"
  fi
}

remove_container_if_exists() {
  local name=$1
  if docker ps -a --format '{{.Names}}' | grep -Fxq "${name}"; then
    log "Removing existing container ${name}"
    docker rm -f "${name}" >/dev/null
  fi
}

build_images() {
  if [[ "${SKIP_BUILD:-0}" == "1" ]]; then
    log "Skipping image build (SKIP_BUILD=1)"
    return
  fi

  log "Building collector image ${COLLECTOR_IMAGE}"
  docker build \
    --build-arg VERSION="${VERSION_TAG}" \
    --build-arg GIT_COMMIT="${GIT_COMMIT}" \
    --build-arg BUILD_TIME="${BUILD_TIME}" \
    -f "${REPO_ROOT}/build/images/Dockerfile.collector" \
    -t "${COLLECTOR_IMAGE}" \
    "${REPO_ROOT}"

  log "Building ingestor image ${INGESTOR_IMAGE}"
  docker build \
    --build-arg VERSION="${VERSION_TAG}" \
    --build-arg GIT_COMMIT="${GIT_COMMIT}" \
    --build-arg BUILD_TIME="${BUILD_TIME}" \
    -f "${REPO_ROOT}/build/images/Dockerfile.ingestor" \
    -t "${INGESTOR_IMAGE}" \
    "${REPO_ROOT}"
}

start_clickhouse() {
  remove_container_if_exists "${CLICKHOUSE_CONTAINER}"
  log "Starting ClickHouse container"
  docker run -d \
    --name "${CLICKHOUSE_CONTAINER}" \
    --network "${NETWORK_NAME}" \
    --network-alias clickhouse \
    -v "${CLICKHOUSE_DATA}:/var/lib/clickhouse" \
    -p 8123:8123 \
    -p 9000:9000 \
    -p 9009:9009 \
    "${CLICKHOUSE_IMAGE}"

  wait_for_clickhouse
  init_clickhouse_db
}

wait_for_clickhouse() {
  log "Waiting for ClickHouse to become ready"
  local retry=0
  until docker exec "${CLICKHOUSE_CONTAINER}" clickhouse-client --query "SELECT 1" >/dev/null 2>&1; do
    retry=$((retry + 1))
    if (( retry > 60 )); then
      err "ClickHouse did not become ready"
      docker logs "${CLICKHOUSE_CONTAINER}" >&2
      exit 1
    fi
    sleep 2
  done
}

init_clickhouse_db() {
  log "Creating ClickHouse databases (${CLICKHOUSE_DB}, ${LOGS_DB})"
  docker exec "${CLICKHOUSE_CONTAINER}" clickhouse-client --query "CREATE DATABASE IF NOT EXISTS ${CLICKHOUSE_DB}" >/dev/null
  docker exec "${CLICKHOUSE_CONTAINER}" clickhouse-client --query "CREATE DATABASE IF NOT EXISTS ${LOGS_DB}" >/dev/null
}

start_ingestor() {
  remove_container_if_exists "${INGESTOR_CONTAINER}"
  log "Starting ingestor container"
  docker run -d \
    --name "${INGESTOR_CONTAINER}" \
    --network "${NETWORK_NAME}" \
    --network-alias ingestor \
    -v "${INGESTOR_DATA}:/var/lib/adx-mon/ingestor" \
    -p 9090:9090 \
    -p 9091:9091 \
    "${INGESTOR_IMAGE}" \
    --storage-dir /var/lib/adx-mon/ingestor \
    --storage-backend clickhouse \
    --metrics-kusto-endpoints "${CLICKHOUSE_DB}=clickhouse://clickhouse:9000" \
    --logs-kusto-endpoints "${LOGS_DB}=clickhouse://clickhouse:9000" \
    --max-segment-size $((64 * 1024 * 1024)) \
    --max-transfer-size $((64 * 1024 * 1024))

  log "Waiting briefly for ingestor startup"
  sleep 5
}

start_collector() {
  remove_container_if_exists "${COLLECTOR_CONTAINER}"
  log "Starting collector container"
  docker run -d \
    --name "${COLLECTOR_CONTAINER}" \
    --network "${NETWORK_NAME}" \
    --network-alias collector \
  -v "${COLLECTOR_DATA}:/var/lib/adx-mon/collector" \
  -v "${COLLECTOR_CONFIG}:${CONFIG_MOUNT}:ro" \
    -p 8080:8080 \
    -p 4317:4317 \
    -p 4318:4318 \
    "${COLLECTOR_IMAGE}" \
    --storage-backend clickhouse \
  --config "${CONFIG_MOUNT}"
}

start_stack() {
  require_docker
  prepare_dirs
  write_collector_config
  create_network
  build_images
  start_clickhouse
  start_ingestor
  start_collector
  log "Stack is ready. Endpoints:\n  - ClickHouse SQL:   http://localhost:8123\n  - ClickHouse Native: clickhouse://localhost:9000\n  - Ingestor HTTPS:   https://localhost:9090 (self-signed)\n  - Metrics:          http://localhost:9091/metrics\n  - Collector HTTP:   http://localhost:8080\n  - Collector OTLP:   http://localhost:4318, grpc://localhost:4317"
}

stop_container() {
  local name=$1
  if docker ps --format '{{.Names}}' | grep -Fxq "${name}"; then
    log "Stopping ${name}"
    docker stop "${name}" >/dev/null
  fi
  if docker ps -a --format '{{.Names}}' | grep -Fxq "${name}"; then
    docker rm "${name}" >/dev/null
  fi
}

stop_stack() {
  require_docker
  stop_container "${COLLECTOR_CONTAINER}"
  stop_container "${INGESTOR_CONTAINER}"
  stop_container "${CLICKHOUSE_CONTAINER}"
}

cleanup_stack() {
  stop_stack
  log "Removing data directories"
  rm -rf "${STACK_DIR}"
}

status_stack() {
  require_docker
  docker ps --filter "name=${CLICKHOUSE_CONTAINER}" --filter "name=${INGESTOR_CONTAINER}" --filter "name=${COLLECTOR_CONTAINER}"
}

logs_stack() {
  require_docker
  local target=${1:-all}
  case "${target}" in
    clickhouse) docker logs -f "${CLICKHOUSE_CONTAINER}" ;;
    ingestor) docker logs -f "${INGESTOR_CONTAINER}" ;;
    collector) docker logs -f "${COLLECTOR_CONTAINER}" ;;
    all) docker logs -f "${CLICKHOUSE_CONTAINER}" & docker logs -f "${INGESTOR_CONTAINER}" & docker logs -f "${COLLECTOR_CONTAINER}" & wait ;;
    *) err "Unknown logs target ${target}"; exit 1 ;;
  esac
}

usage() {
  cat <<EOF
Usage: $(basename "$0") <command> [args]

Commands:
  start             Build images (unless SKIP_BUILD=1) and launch ClickHouse, ingestor, collector
  stop              Stop and remove the stack containers (data preserved)
  status            Show container status for the stack
  logs [component]  Tail logs (component: clickhouse|ingestor|collector|all)
  cleanup           Stop containers and delete generated data/config
  help              Show this help message

Environment Overrides:
  SKIP_BUILD=1              Skip docker build steps during start
  VERSION_TAG=<tag>         Version string injected into images (default: dev)
  CLICKHOUSE_IMAGE=<image>  Override ClickHouse image tag (default: ${CLICKHOUSE_IMAGE})
  CLICKHOUSE_DB=<name>      Metrics database name (default: ${CLICKHOUSE_DB})
  LOGS_DB=<name>            Logs database name (default: ${LOGS_DB})
  FORCE_REGENERATE_CONFIG=1 Re-write collector config even if it exists
EOF
}

case "${command}" in
  start) start_stack ;;
  stop) stop_stack ;;
  cleanup) cleanup_stack ;;
  status) status_stack ;;
  logs) logs_stack "$@" ;;
  help|--help|-h) usage ;;
  *) err "Unknown command: ${command}"; usage; exit 1 ;;
esac
