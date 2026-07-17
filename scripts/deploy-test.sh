#!/usr/bin/env bash
# Deploy the current workspace build to the local test service
# reverse-proxied at https://3x.hepdd.com
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

BIN_DST="${GOWEBSSH_BIN:-/opt/go-webssh/go-webssh}"
CONFIG="${GOWEBSSH_CONFIG:-/etc/go-webssh/config.yaml}"
DATA_DIR="${GOWEBSSH_DATA_DIR:-/var/lib/go-webssh}"
SERVICE="${GOWEBSSH_SERVICE_NAME:-go-webssh}"
LISTEN_HEALTH="${GOWEBSSH_HEALTH_URL:-http://127.0.0.1:18082}"
VERSION="${GOWEBSSH_VERSION_LABEL:-0.5.6-dev}"
THEMES_DIR="${DATA_DIR}/themes"

export GOCACHE="${GOCACHE:-/tmp/go-cache}"
export GOMODCACHE="${GOMODCACHE:-/tmp/go-modcache}"
mkdir -p "${GOCACHE}" "${GOMODCACHE}"

if [[ ${EUID} -ne 0 ]]; then
  echo "Run as root (needed to install binary and restart systemd)." >&2
  exit 1
fi

log() { printf '\033[1;34m[deploy-test]\033[0m %s\n' "$*"; }

resolve_go() {
  if [[ -n ${GOWEBSSH_GO:-} && -x ${GOWEBSSH_GO} ]]; then
    printf '%s\n' "${GOWEBSSH_GO}"
    return
  fi
  if command -v go >/dev/null 2>&1; then
    command -v go
    return
  fi
  local candidate
  candidate="$(ls -d /tmp/go-modcache/golang.org/toolchain@*/bin/go 2>/dev/null | tail -n1 || true)"
  if [[ -n ${candidate} && -x ${candidate} ]]; then
    printf '%s\n' "${candidate}"
    return
  fi
  echo "Go toolchain not found." >&2
  exit 1
}

GO_BIN="$(resolve_go)"
log "Using Go: $("${GO_BIN}" version)"

log "Running tests..."
"${GO_BIN}" test ./...

log "Building ${VERSION}..."
tmp_bin="$(mktemp)"
trap 'rm -f "${tmp_bin}"' EXIT
CGO_ENABLED=0 "${GO_BIN}" build -trimpath \
  -ldflags "-s -w -X main.version=${VERSION}" \
  -o "${tmp_bin}" ./cmd/webssh

log "Installing themes to ${THEMES_DIR}"
install -d -m 0750 -o go-webssh -g go-webssh "${THEMES_DIR}"
if [[ -d "${ROOT}/themes" ]]; then
  # Refresh bundled templates without deleting custom user themes.
  find "${ROOT}/themes" -maxdepth 1 -type f -name '*.json' -exec cp -a {} "${THEMES_DIR}/" \;
  chown -R go-webssh:go-webssh "${THEMES_DIR}"
  find "${THEMES_DIR}" -type f -name '*.json' -exec chmod 0644 {} \;
fi

if [[ -f ${CONFIG} ]] && ! grep -q 'themes_dir' "${CONFIG}"; then
  log "Appending ui.themes_dir to ${CONFIG}"
  printf '\nui:\n  themes_dir: "%s"\n' "${THEMES_DIR}" >> "${CONFIG}"
fi

log "Installing binary to ${BIN_DST}"
install -d -m 0755 "$(dirname "${BIN_DST}")"
install -m 0755 "${tmp_bin}" "${BIN_DST}"

log "Restarting ${SERVICE}"
systemctl restart "${SERVICE}"
sleep 1
systemctl is-active --quiet "${SERVICE}"

health="$(curl -fsS --max-time 5 "${LISTEN_HEALTH}/api/healthz" || true)"
if [[ ${health} != *'"ok":true'* ]]; then
  journalctl -u "${SERVICE}" -n 30 --no-pager || true
  echo "Health check failed: ${health}" >&2
  exit 1
fi

ui="$(curl -fsS --max-time 5 "${LISTEN_HEALTH}/api/config/ui")"
log "Deployed ${VERSION}"
log "Health: ${health}"
log "UI config: ${ui}"
log "Public URL: https://3x.hepdd.com/"
