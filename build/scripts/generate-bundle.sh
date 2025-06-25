#!/bin/bash
set -euo pipefail

# Script to generate bundle.sh from the k8s directory
# Uses Docker for cross-platform compatibility (works on Linux, macOS, etc.)

SCRIPT_DIR="$(dirname "${BASH_SOURCE[0]}")"
K8S_DIR="$(cd "$SCRIPT_DIR/../k8s" && pwd)"

echo "Generating bundle.sh from $K8S_DIR using Docker"

# Check if Docker is available
if ! command -v docker &> /dev/null; then
    echo "Error: Docker is required but not found. Please install Docker."
    exit 1
fi

# Run bundle generation in Docker container for cross-platform compatibility
docker run --rm -v "$K8S_DIR:/build" ubuntu:latest bash -eu -o pipefail -c '
  cd /build

  GZIP=-n \
  tar --sort=name \
      --owner=0 --group=0 --numeric-owner \
      --mtime="UTC 1970-01-01 00:00:00" \
      --exclude=bundle.sh \
      -cf - . \
  | gzip -n > /tmp/bundle.tar.gz

  base64 /tmp/bundle.tar.gz > /tmp/bundle.tar.gz.b64

  {
    printf "#!/bin/bash\nbase64 -d << \"EOF\" | tar xz\n"
    cat /tmp/bundle.tar.gz.b64
    printf "EOF\n./setup.sh\n"
  } > bundle.sh

  chmod +x bundle.sh
'

echo "Bundle generated successfully at $K8S_DIR/bundle.sh"