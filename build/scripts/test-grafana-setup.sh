#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SETUP_SCRIPT="${SETUP_SCRIPT:-"$SCRIPT_DIR/../k8s/setup.sh"}"
TEST_DIR=$(mktemp -d)
trap 'rm -r "$TEST_DIR"' EXIT
export TEST_DIR

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

az() {
    echo "$*" >> "$TEST_DIR/commands"
    case "$*" in
        "account show"*) ;;
        "account get-access-token"*) echo 9999999999 ;;
        "extension show"*) ;;
        "graph query"*)
            case "$*" in
                *Microsoft.ContainerService*) echo '{"data":[{"resourceGroup":"rg","subscriptionId":"sub","location":"eastus"}]}' ;;
                *Microsoft.Kusto*) echo '{"data":[{"resourceGroup":"rg","subscriptionId":"sub","location":"eastus","properties_uri":"https://adx.example"}]}' ;;
                *Microsoft.Dashboard*)
                    if [[ "$SCENARIO" == "new-instance" ]]; then
                        echo '{"data":[]}'
                    else
                        echo '{"data":[{"resourceGroup":"rg","subscriptionId":"grafana-sub","location":"eastus","properties_endpoint":"https://grafana.example"}]}'
                    fi
                    ;;
            esac
            ;;
        "aks show"*) echo '"node-identity"' ;;
        "aks get-credentials"*) ;;
        "kusto database show"*) echo Metrics ;;
        "kusto database list-principal"*) echo principal ;;
        "kusto database add-principal"*) ;;
        "grafana create"*) ;;
        "grafana show"*)
            if [[ "$*" == *identity.principalId* ]]; then
                echo '"grafana-identity"'
            else
                echo '{"properties":{"provisioningState":"Succeeded","endpoint":"https://grafana.example"}}'
            fi
            ;;
        "grafana data-source list"*)
            echo probe >> "$TEST_DIR/probes"
            if [[ "$SCENARIO" == "timeout" || $(wc -l < "$TEST_DIR/probes") -lt 3 ]]; then
                echo "Grafana API unavailable" >&2
                return 7
            fi
            ;;
        "grafana data-source show"*) return 3 ;;
        "grafana data-source create"*)
            [[ $(wc -l < "$TEST_DIR/probes") -eq 3 ]] || return 9
            echo create >> "$TEST_DIR/sources"
            if [[ "$SCENARIO" == "source-failure" || $(wc -l < "$TEST_DIR/sources") -lt 2 ]]; then
                echo "Data-source creation unavailable" >&2
                return 8
            fi
            ;;
        "grafana dashboard create"*)
            local definition=""
            while (($#)); do
                if [[ "$1" == "--definition" ]]; then
                    definition="${2#@}"
                    break
                fi
                shift
            done
            [[ -n "$definition" ]] || return 10
            jq -e '
                (.templating.list[] | select(.name == "Datasource") | .current.value == "adx") and
                (.templating.list[] | select(.name == "Cluster") | .current.value == "aks")
            ' "$definition" > /dev/null || return 11
            echo "$definition" >> "$TEST_DIR/dashboards"
            if [[ "$SCENARIO" == "dashboard-failure" || $(wc -l < "$TEST_DIR/dashboards") -lt 3 ]]; then
                echo "Dashboard creation unavailable" >&2
                return 12
            fi
            ;;
        *) echo "Unexpected az command: $*" >&2; return 99 ;;
    esac
}

kubectl() { if [[ "$*" == "apply -f -" ]]; then cat > /dev/null; fi; }
envsubst() { cat; }
sleep() { echo "$*" >> "$TEST_DIR/sleeps"; }
export -f az kubectl envsubst sleep

for SCENARIO in existing-instance new-instance timeout source-failure dashboard-failure; do
    export SCENARIO
    rm -f "$TEST_DIR/commands" "$TEST_DIR/probes" "$TEST_DIR/sources" "$TEST_DIR/dashboards" "$TEST_DIR/sleeps"
    status=0
    if [[ "$SCENARIO" == "new-instance" ]]; then
        printf 'aks\ny\nadx\ny\ny\ngrafana\ny\n' |
            bash "$SETUP_SCRIPT" > "$TEST_DIR/output" 2>&1 || status=$?
    else
        bash "$SETUP_SCRIPT" -c 'aks sub rg' -k 'adx sub rg' -g 'grafana grafana-sub rg' \
            > "$TEST_DIR/output" 2>&1 || status=$?
    fi

    case "$SCENARIO" in
        existing-instance|new-instance)
            [[ "$status" -eq 0 ]] || { cat "$TEST_DIR/output"; fail "$SCENARIO exited with $status"; }
            [[ $(wc -l < "$TEST_DIR/probes") -eq 3 ]] || fail "Readiness did not retry"
            [[ $(wc -l < "$TEST_DIR/sources") -eq 2 ]] || fail "Data-source creation did not retry"
            [[ $(wc -l < "$TEST_DIR/dashboards") -eq 7 ]] || fail "Not all five dashboards were imported with retries"
            [[ $(grep -c '^30$' "$TEST_DIR/sleeps") -eq 2 ]] || fail "Incorrect readiness delay"
            [[ $(grep -c '^10$' "$TEST_DIR/sleeps") -eq 3 ]] || fail "Incorrect creation delay"
            while read -r dashboard; do
                [[ ! -e "$dashboard" ]] || fail "Temporary dashboard was not removed"
            done < "$TEST_DIR/dashboards"
            ;;
        timeout)
            [[ "$status" -eq 7 ]] || fail "Readiness timeout did not propagate the CLI failure"
            [[ $(wc -l < "$TEST_DIR/probes") -eq 30 ]] || fail "Readiness did not stop at 30 attempts"
            [[ $(wc -l < "$TEST_DIR/sleeps") -eq 29 ]] || fail "Readiness slept after its last attempt"
            [[ ! -e "$TEST_DIR/sources" && ! -e "$TEST_DIR/dashboards" ]] || fail "Setup continued after readiness timed out"
            ;;
        source-failure)
            [[ "$status" -eq 8 ]] || fail "Data-source failure did not propagate"
            [[ $(wc -l < "$TEST_DIR/sources") -eq 3 ]] || fail "Incorrect data-source retry limit"
            [[ ! -e "$TEST_DIR/dashboards" ]] || fail "Dashboards imported after data-source failure"
            ;;
        dashboard-failure)
            [[ "$status" -ne 0 ]] || fail "Dashboard exhaustion reported success"
            [[ $(wc -l < "$TEST_DIR/dashboards") -eq 3 ]] || fail "Incorrect dashboard retry limit"
            read -r dashboard < "$TEST_DIR/dashboards"
            [[ ! -e "$dashboard" ]] || fail "Failed dashboard left a temporary file"
            ;;
    esac

    if [[ "$status" -ne 0 ]]; then
        grep -q 'failed after .* attempts' "$TEST_DIR/output" || fail "Missing exhaustion error"
        if grep -q 'Successfully deployed' "$TEST_DIR/output"; then
            fail "Failure reported a successful deployment"
        fi
    fi
    grep -q 'Grafana API unavailable' "$TEST_DIR/output" || fail "CLI errors were hidden"
    echo "PASS: $SCENARIO"
done

source "$SCRIPT_DIR/../k8s/grafana.sh"
attempts=0
immediate_success() { attempts=$((attempts + 1)); }
retry_grafana_command "Immediate success" 3 10 immediate_success
[[ "$attempts" -eq 1 ]] || fail "Successful command retried unnecessarily"
echo "PASS: immediate success"
