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

echo "Building ncloud_exporter v${VERSION} (${RELEASE_DATE})"

GOOS=linux GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o "${SCRIPT_DIR}/bin/amd64/ncloud_exporter"
echo "  -> bin/amd64/ncloud_exporter"

GOOS=linux GOARCH=arm64 go build -ldflags "${LDFLAGS}" -o "${SCRIPT_DIR}/bin/arm64/ncloud_exporter"
echo "  -> bin/arm64/ncloud_exporter"

echo "Done."
