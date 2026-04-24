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

cd "${REPO_ROOT}"

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
CGO_ENABLED=0 "${GO_BIN}" build -trimpath -ldflags="-s -w" -o "${BUILD_DIR}/${BINARY_NAME}" ./cmd/empirebusd

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
sudo chown -R xtura:xtura "${INSTALL_ROOT}" /var/lib/xtura

echo "==> Restarting ${SERVICE_NAME}"
sudo systemctl daemon-reload
sudo systemctl restart "${SERVICE_NAME}.service"
sudo systemctl --no-pager --full status "${SERVICE_NAME}.service"
echo "==> Recent ${SERVICE_NAME} logs"
sudo journalctl -u "${SERVICE_NAME}.service" -n 50 --no-pager

echo "==> Health check"
curl --fail --silent --show-error http://127.0.0.1:8080/v1/health
echo

if [[ "${1:-HEAD}" != "HEAD" ]]; then
  echo "==> Returning repo to ${CURRENT_SHA}"
  git checkout "${CURRENT_BRANCH}"
fi

echo "Deploy complete: ${TARGET_SHA}"
