#!/bin/bash

retry_grafana_command() {
    local description="$1"
    local max_attempts="$2"
    local delay="$3"
    shift 3
    local attempt status

    for ((attempt = 1; attempt <= max_attempts; attempt++)); do
        if "$@"; then
            return 0
        else
            status=$?
        fi
        if ((attempt < max_attempts)); then
            echo "$description failed (attempt $attempt/$max_attempts). Retrying in $delay seconds..." >&2
            sleep "$delay"
        fi
    done

    echo "$description failed after $max_attempts attempts. Exiting." >&2
    return "$status"
}
