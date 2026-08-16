#!/usr/bin/env bash
set -euo pipefail

PI_HOST="${PI_HOST:-jones-pi.taile19bc2.ts.net}"
PI_USER="${PI_USER:-$(id -un)}"
PI_PORT="${PI_PORT:-22}"
REMOTE_REPO="${REMOTE_REPO:-/home/${PI_USER}/development/xtura-automation}"
ENVIRONMENT="${ENVIRONMENT:-prod}"
TARGET_SHA="${1:-HEAD}"

remote_supports_environment() {
  ssh -p "${PI_PORT}" "${PI_USER}@${PI_HOST}" \
    "grep -qF 'ENVIRONMENT=' '${REMOTE_REPO}/scripts/deploy/deploy-on-pi.sh'" 2>/dev/null
}

if [[ "${ENVIRONMENT}" == "staging" ]] && ! remote_supports_environment; then
  echo "error: the Pi checkout's deploy script does not support ENVIRONMENT=staging;" >&2
  echo "       check out a branch that includes the staging deployment work, then retry." >&2
  exit 1
fi

ssh -p "${PI_PORT}" "${PI_USER}@${PI_HOST}" "\
  cd '${REMOTE_REPO}' && \
  ENVIRONMENT='${ENVIRONMENT}' ./scripts/deploy/deploy-on-pi.sh '${TARGET_SHA}'"
