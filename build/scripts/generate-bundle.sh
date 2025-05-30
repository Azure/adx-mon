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
docker run --rm -v "$K8S_DIR:/build" ubuntu:latest bash -c '
    cd /build || exit 1
    
    # Remove existing bundle.sh if present
    rm -f ./bundle.sh
    
    # Create tarball excluding bundle.sh, with deterministic timestamps
    tar czf /tmp/bundle.tar.gz --exclude=bundle.sh --mtime="1970-01-01 00:00:00" .
    
    # Encode to base64
    base64 /tmp/bundle.tar.gz > /tmp/bundle.tar.gz.b64
    
    # Generate new bundle.sh
    echo "#!/bin/bash" > bundle.sh
    echo "base64 -d << \"EOF\" | tar xz" >> bundle.sh
    cat /tmp/bundle.tar.gz.b64 >> bundle.sh
    echo "EOF" >> bundle.sh
    echo "./setup.sh" >> bundle.sh
    
    # Make it executable
    chmod +x bundle.sh
'

echo "Bundle generated successfully at $K8S_DIR/bundle.sh"