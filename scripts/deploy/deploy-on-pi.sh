#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
INSTALL_ROOT="/opt/xtura"
CONFIG_PATH="/var/lib/xtura/config.yaml"
SERVICE_NAME="empirebusd"
BINARY_NAME="empirebusd"
GO_BIN="${GO_BIN:-go}"
SERVICE_UNIT_SOURCE="${REPO_ROOT}/ops/systemd/empirebusd.service"
SERVICE_UNIT_DEST="/etc/systemd/system/empirebusd.service"
SUDOERS_TIMEZONE_SOURCE="${REPO_ROOT}/ops/sudoers/xtura-timezone"
SUDOERS_TIMEZONE_DEST="/etc/sudoers.d/xtura-timezone"

cd "${REPO_ROOT}"

is_raspberry_pi() {
  [[ -r /proc/device-tree/model ]] || return 1
  tr -d '\0' </proc/device-tree/model | grep -qi "raspberry pi"
}

if [[ "$(uname -s)" != "Linux" ]] || ! is_raspberry_pi; then
  echo "deploy-on-pi.sh must be run on a Raspberry Pi Linux host; refusing to install or enable ${SERVICE_NAME} here." >&2
  exit 1
fi

if ! command -v "${GO_BIN}" >/dev/null 2>&1; then
  echo "go binary not found: ${GO_BIN}" >&2
  exit 1
fi

if [[ ! -f "${CONFIG_PATH}" ]]; then
  echo "config file not found: ${CONFIG_PATH}" >&2
  exit 1
fi

echo "==> Fetching latest refs"
git fetch origin

CURRENT_BRANCH="$(git branch --show-current)"
CURRENT_SHA="$(git rev-parse HEAD)"
TARGET_SHA="${1:-HEAD}"

if [[ "${TARGET_SHA}" == "HEAD" ]]; then
  git pull --ff-only origin "${CURRENT_BRANCH}"
  TARGET_SHA="$(git rev-parse HEAD)"
else
  git checkout --detach "${TARGET_SHA}"
fi

echo "==> Running tests"
"${GO_BIN}" test ./...

echo "==> Building ${BINARY_NAME}"
BUILD_DIR="$(mktemp -d)"
trap 'rm -rf "${BUILD_DIR}"' EXIT
SHORT_SHA="${TARGET_SHA:0:7}"
BUILD_LDFLAGS="-s -w"
BUILD_LDFLAGS="${BUILD_LDFLAGS} -X empirebus-tests/service/buildinfo.GitSHA=${SHORT_SHA}"
BUILD_LDFLAGS="${BUILD_LDFLAGS} -X empirebus-tests/service/buildinfo.DeployedAt=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
CGO_ENABLED=0 "${GO_BIN}" build -trimpath -ldflags="${BUILD_LDFLAGS}" -o "${BUILD_DIR}/${BINARY_NAME}" ./cmd/empirebusd

RELEASES_DIR="${INSTALL_ROOT}/releases"
RELEASE_DIR="${RELEASES_DIR}/${TARGET_SHA}"
CURRENT_LINK="${INSTALL_ROOT}/current"

echo "==> Installing release ${TARGET_SHA}"
sudo mkdir -p "${RELEASES_DIR}" /var/lib/xtura
sudo rm -rf "${RELEASE_DIR}"
sudo mkdir -p "${RELEASE_DIR}"
sudo install -m 0755 "${BUILD_DIR}/${BINARY_NAME}" "${RELEASE_DIR}/${BINARY_NAME}"
sudo ln -sfn "${RELEASE_DIR}" "${CURRENT_LINK}"
sudo install -m 0644 "${SERVICE_UNIT_SOURCE}" "${SERVICE_UNIT_DEST}"
sudo install -m 0440 "${SUDOERS_TIMEZONE_SOURCE}" "${SUDOERS_TIMEZONE_DEST}"
sudo chown -R xtura:xtura "${INSTALL_ROOT}" /var/lib/xtura

echo "==> Migrating garmin.ws_url to the SERV Ethernet endpoint"
LEGACY_WS_URL="ws://192.168.1.1:8888/ws"
SERV_WS_URL="ws://172.16.11.7:8888/ws"
if sudo grep -q "${LEGACY_WS_URL}" "${CONFIG_PATH}"; then
  sudo cp "${CONFIG_PATH}" "${CONFIG_PATH}.bak-ws-migration"
  sudo sed -i "s#${LEGACY_WS_URL}#${SERV_WS_URL}#g" "${CONFIG_PATH}"
  echo "Migrated garmin.ws_url to ${SERV_WS_URL} in ${CONFIG_PATH} (backup: ${CONFIG_PATH}.bak-ws-migration)"
else
  echo "garmin.ws_url in ${CONFIG_PATH} left unchanged"
fi

echo "==> Enabling ${SERVICE_NAME} on boot"
sudo systemctl daemon-reload
sudo systemctl enable "${SERVICE_NAME}.service"

echo "==> Restarting ${SERVICE_NAME}"
sudo systemctl restart "${SERVICE_NAME}.service"
sudo systemctl --no-pager --full status "${SERVICE_NAME}.service"
echo "==> Recent ${SERVICE_NAME} logs"
sudo journalctl -u "${SERVICE_NAME}.service" -n 50 --no-pager

echo "==> Health check"
HEALTH_OUTPUT="$(mktemp)"
for attempt in {1..30}; do
  if curl --fail --silent --show-error --max-time 2 http://127.0.0.1/v1/health >"${HEALTH_OUTPUT}" 2>&1; then
    cat "${HEALTH_OUTPUT}"
    echo
    break
  fi
  if [[ "${attempt}" == 30 ]]; then
    cat "${HEALTH_OUTPUT}" >&2
    echo "health check failed after ${attempt} attempts" >&2
    exit 1
  fi
  sleep 1
done

if [[ "${1:-HEAD}" != "HEAD" ]]; then
  echo "==> Returning repo to ${CURRENT_SHA}"
  git checkout "${CURRENT_BRANCH}"
fi

echo "Deploy complete: ${TARGET_SHA}"
