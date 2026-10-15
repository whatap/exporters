#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BUILD_FILE="${SCRIPT_DIR}/build.txt"

if [ ! -f "${BUILD_FILE}" ]; then
    echo "Error: build.txt not found"
    exit 1
fi

VERSION=$(grep '^version=' "${BUILD_FILE}" | cut -d'=' -f2)
RELEASE_DATE=$(grep '^release_date=' "${BUILD_FILE}" | cut -d'=' -f2)

if [ -z "${VERSION}" ] || [ -z "${RELEASE_DATE}" ]; then
    echo "Error: version or release_date not set in build.txt"
    exit 1
fi

LDFLAGS="-X main.version=${VERSION} -X main.releaseDate=${RELEASE_DATE}"

echo "Building ec2_state_exporter v${VERSION} (${RELEASE_DATE})"

cd "${SCRIPT_DIR}"

# Static binaries: the exporter runs on plain EC2 hosts and minimal container images.
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o "${SCRIPT_DIR}/bin/amd64/ec2_state_exporter" .
echo "  -> bin/amd64/ec2_state_exporter"

CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "${LDFLAGS}" -o "${SCRIPT_DIR}/bin/arm64/ec2_state_exporter" .
echo "  -> bin/arm64/ec2_state_exporter"

echo "Done."
