#!/usr/bin/env bash
set -euo pipefail

PI_HOST="${PI_HOST:-jones-pi.taile19bc2.ts.net}"
PI_USER="${PI_USER:-$(id -un)}"
PI_PORT="${PI_PORT:-22}"
REMOTE_REPO="${REMOTE_REPO:-/home/${PI_USER}/src/xtura-automation}"
TARGET_SHA="${1:-HEAD}"

ssh -p "${PI_PORT}" "${PI_USER}@${PI_HOST}" "\
  cd '${REMOTE_REPO}' && \
  ./scripts/deploy/deploy-on-pi.sh '${TARGET_SHA}'"
