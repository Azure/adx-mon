#!/bin/bash
set -euo pipefail

# Script to generate bundle.sh from the k8s directory
# This replaces the Docker-based approach with native tools

SCRIPT_DIR="$(dirname "${BASH_SOURCE[0]}")"
K8S_DIR="$(cd "$SCRIPT_DIR/../k8s" && pwd)"
BUNDLE_FILE="$K8S_DIR/bundle.sh"
TEMP_DIR="$(mktemp -d)"

cleanup() {
    rm -rf "$TEMP_DIR"
}
trap cleanup EXIT

echo "Generating bundle.sh from $K8S_DIR"

cd "$TEMP_DIR"

# Create tarball excluding the existing bundle.sh
# Use --mtime to ensure reproducible builds
tar czf bundle.tar.gz --exclude=bundle.sh --mtime='1970-01-01 00:00:00' -C "$K8S_DIR" .

# Encode to base64
base64 bundle.tar.gz > bundle.tar.gz.b64

# Generate new bundle.sh
{
    echo '#!/bin/bash'
    echo 'base64 -d << "EOF" | tar xz'
    cat bundle.tar.gz.b64
    echo 'EOF'
    echo './setup.sh'
} > "$BUNDLE_FILE"

# Make it executable
chmod +x "$BUNDLE_FILE"

echo "Bundle generated successfully at $BUNDLE_FILE"