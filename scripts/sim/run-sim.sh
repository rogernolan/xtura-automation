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

stop_existing_listener() {
  local port="$1"
  local pids
  pids="$(lsof -tiTCP:"${port}" -sTCP:LISTEN 2>/dev/null || true)"
  if [[ -z "${pids}" ]]; then
    return
  fi
  echo "==> Stopping existing listener(s) on :${port}: ${pids//$'\n'/ }"
  kill ${pids} 2>/dev/null || true
  for _ in {1..20}; do
    if ! lsof -tiTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1; then
      return
    fi
    sleep 0.1
  done
  pids="$(lsof -tiTCP:"${port}" -sTCP:LISTEN 2>/dev/null || true)"
  [[ -z "${pids}" ]] || kill -9 ${pids} 2>/dev/null || true
}

stop_existing_listener 8090
stop_existing_listener 8091

BUILD_DIR="$(mktemp -d)"
cd "${REPO_ROOT}"

echo "==> Building servsim and empirebusd"
go build -o "${BUILD_DIR}/servsim" ./cmd/servsim
go build -o "${BUILD_DIR}/empirebusd" ./cmd/empirebusd

echo "==> Starting servsim (capture=${CAPTURE})"
"${BUILD_DIR}/servsim" -listen :8090 -capture "${CAPTURE}" &
SERVSIM_PID=$!
cleanup() {
  [[ -z "${EMPIREBUSD_PID:-}" ]] || kill "${EMPIREBUSD_PID}" 2>/dev/null || true
  [[ -z "${SERVSIM_PID:-}" ]] || kill "${SERVSIM_PID}" 2>/dev/null || true
  rm -rf "${BUILD_DIR}"
}
trap cleanup EXIT INT TERM
cp "${REPO_ROOT}/config.sim.yaml" "${BUILD_DIR}/config.sim.yaml"

for _ in {1..50}; do
  if curl --silent --output /dev/null --max-time 1 http://127.0.0.1:8090/; then
    break
  fi
  sleep 0.1
done

echo "==> Starting empirebusd (config=temporary sim copy)"
echo "    UI/API: http://localhost:8091"
SENTRY_DISABLED=1 XTURA_ENVIRONMENT=simulation XTURA_SIM_SWITCHBOT=1 "${BUILD_DIR}/empirebusd" -config "${BUILD_DIR}/config.sim.yaml" &
EMPIREBUSD_PID=$!
wait "${EMPIREBUSD_PID}"
