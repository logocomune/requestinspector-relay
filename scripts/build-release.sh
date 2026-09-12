#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${ROOT_DIR}/dist/release"

command -v go >/dev/null 2>&1 || { echo "go is required" >&2; exit 1; }
command -v git >/dev/null 2>&1 || { echo "git is required" >&2; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "npm is required" >&2; exit 1; }

BUILD_VERSION="$(git -C "${ROOT_DIR}" describe --tags --always --dirty)"
BUILD_COMMIT="$(git -C "${ROOT_DIR}" rev-parse HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if [[ ! -d "${ROOT_DIR}/web/node_modules" ]]; then
  echo "Frontend dependencies missing. Run: cd web && npm ci" >&2
  exit 1
fi

mkdir -p "${OUTPUT_DIR}"

echo "Building embedded frontend"
npm --prefix "${ROOT_DIR}/web" run build

build_target() {
  local name="$1"
  local goos="$2"
  local goarch="$3"
  local output_name="$4"
  local goarm="${5:-}"

  local -a environment=(CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}")
  if [[ -n "${goarm}" ]]; then
    environment+=(GOARM="${goarm}")
  fi

  echo "Building ${name}"
  (
    cd "${ROOT_DIR}"
    env "${environment[@]}" \
      go build -trimpath -ldflags="-s -w -X main.version=${BUILD_VERSION} -X main.commit=${BUILD_COMMIT} -X main.date=${BUILD_DATE}" -o "${OUTPUT_DIR}/${output_name}" ./cmd/reqrelay
  )
}

build_target "linux-amd64" linux amd64 reqrelay-linux-amd64
build_target "windows-amd64" windows amd64 reqrelay-windows-amd64.exe
build_target "darwin-amd64" darwin amd64 reqrelay-darwin-amd64
build_target "darwin-arm64" darwin arm64 reqrelay-darwin-arm64
build_target "linux-armv7" linux arm reqrelay-linux-armv7 7
build_target "linux-arm64" linux arm64 reqrelay-linux-arm64

echo "Release binaries written to ${OUTPUT_DIR}"
