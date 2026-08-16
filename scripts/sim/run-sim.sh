#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

CAPTURE="${1:-}"
if [[ -z "${CAPTURE}" ]]; then
  CAPTURE="$(ls -1 "${REPO_ROOT}"/captures/garmin-ws-*.ndjson 2>/dev/null | tail -1 || true)"
fi
if [[ -z "${CAPTURE}" ]]; then
  echo "no capture found; pass one explicitly, e.g.:" >&2
  echo "  ${0} captures/garmin-ws-YYYYMMDD-HHMMSS.ndjson" >&2
  exit 1
fi
if [[ ! -f "${CAPTURE}" ]]; then
  echo "capture not found: ${CAPTURE}" >&2
  exit 1
fi

BUILD_DIR="$(mktemp -d)"
cd "${REPO_ROOT}"

echo "==> Building servsim and empirebusd"
go build -o "${BUILD_DIR}/servsim" ./cmd/servsim
go build -o "${BUILD_DIR}/empirebusd" ./cmd/empirebusd

echo "==> Starting servsim (capture=${CAPTURE})"
"${BUILD_DIR}/servsim" -listen :8090 -capture "${CAPTURE}" &
SERVSIM_PID=$!
trap 'kill "${SERVSIM_PID}" 2>/dev/null || true; rm -rf "${BUILD_DIR}"' EXIT

for _ in {1..50}; do
  if curl --silent --output /dev/null --max-time 1 http://127.0.0.1:8090/; then
    break
  fi
  sleep 0.1
done

echo "==> Starting empirebusd (config=config.sim.yaml)"
echo "    UI/API: http://localhost:8091"
"${BUILD_DIR}/empirebusd" -config ./config.sim.yaml
