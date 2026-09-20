#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CPA_DIR="${CPA_DIR:-"${SCRIPT_DIR}/../CPA"}"

GOOS="$(go env GOOS)"
GOARCH="$(go env GOARCH)"

OUTPUT_DIR="${SCRIPT_DIR}/bin/${GOOS}/${GOARCH}"
mkdir -p "${OUTPUT_DIR}"

echo "==> Building unified-model plugin for ${GOOS}/${GOARCH}..."
(
  cd "${SCRIPT_DIR}/go"
  go build -buildmode=c-shared -o "${OUTPUT_DIR}/unified-model.dylib" .
)
echo "==> Build complete: ${OUTPUT_DIR}/unified-model.dylib"

if [[ -d "${CPA_DIR}" ]]; then
  TARGET_DIR="${CPA_DIR}/plugins/${GOOS}/${GOARCH}"
  echo "==> Installing plugin into CPA (${TARGET_DIR})..."
  mkdir -p "${TARGET_DIR}"
  cp "${OUTPUT_DIR}/unified-model.dylib" "${TARGET_DIR}/unified-model.dylib"
  if [[ -f "${OUTPUT_DIR}/unified-model.h" ]]; then
    cp "${OUTPUT_DIR}/unified-model.h" "${TARGET_DIR}/unified-model.h"
  fi
  echo "==> Installed successfully to CPA!"
fi
