#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CPA_DIR="${CPA_DIR:-"${SCRIPT_DIR}/../CPA"}"

PLUGIN_VERSION="$(
  awk -F'"' '/Version:[[:space:]]+"/ { print $2; exit }' "${SCRIPT_DIR}/go/main.go"
)"
if [[ -z "${PLUGIN_VERSION}" ]]; then
  echo "failed to read plugin version from go/main.go" >&2
  exit 1
fi

GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"

case "${GOOS}" in
  darwin) EXT="dylib" ;;
  windows) EXT="dll" ;;
  *) EXT="so" ;;
esac

OUTPUT_DIR="${SCRIPT_DIR}/bin/${GOOS}/${GOARCH}"
OUTPUT_NAME="unified-model-v${PLUGIN_VERSION}.${EXT}"
mkdir -p "${OUTPUT_DIR}"

echo "==> Building unified-model v${PLUGIN_VERSION} for ${GOOS}/${GOARCH}..."
(
  cd "${SCRIPT_DIR}/go"
  GOOS="${GOOS}" GOARCH="${GOARCH}" CGO_ENABLED=1 go build -buildmode=c-shared \
    -o "${OUTPUT_DIR}/${OUTPUT_NAME}" .
)
rm -f "${OUTPUT_DIR}/unified-model-v${PLUGIN_VERSION}.h"
echo "==> Build complete: ${OUTPUT_DIR}/${OUTPUT_NAME}"

if [[ -d "${CPA_DIR}" ]]; then
  TARGET_DIR="${CPA_DIR}/plugins/${GOOS}/${GOARCH}"
  echo "==> Installing plugin into CPA (${TARGET_DIR})..."
  mkdir -p "${TARGET_DIR}"
  cp "${OUTPUT_DIR}/${OUTPUT_NAME}" "${TARGET_DIR}/${OUTPUT_NAME}"
  echo "==> Installed ${OUTPUT_NAME}. CPA hot-reloads versioned plugin files."
fi
