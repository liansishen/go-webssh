#!/usr/bin/env bash
set -Eeuo pipefail

REPOSITORY="${GOWEBSSH_REPOSITORY:-liansishen/go-webssh}"
VERSION="${GOWEBSSH_VERSION:-latest}"
INSTALL_DIR="${GOWEBSSH_INSTALL_DIR:-/usr/local/bin}"
CONFIG_DIR="${GOWEBSSH_CONFIG_DIR:-/etc/go-webssh}"
DATA_DIR="${GOWEBSSH_DATA_DIR:-/var/lib/go-webssh}"
SERVICE_NAME="${GOWEBSSH_SERVICE_NAME:-go-webssh}"
SERVICE_USER="${GOWEBSSH_SERVICE_USER:-go-webssh}"
LISTEN="${GOWEBSSH_LISTEN:-127.0.0.1:8080}"
WEB_USERNAME="${GOWEBSSH_USERNAME:-admin}"
SECURE_COOKIE="${GOWEBSSH_SECURE_COOKIE:-false}"
ALLOW_PRIVATE_RANGES="${GOWEBSSH_ALLOW_PRIVATE_RANGES:-false}"
TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"

log() { printf '\033[1;34m[go-webssh]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[go-webssh] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }

if [[ ${EUID} -ne 0 ]]; then
  fail "Run this installer as root (for example: curl ... | sudo bash)."
fi

[[ ${REPOSITORY} =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || fail "Invalid repository: ${REPOSITORY}"
[[ ${SERVICE_NAME} =~ ^[A-Za-z0-9_.@-]+$ ]] || fail "Invalid service name: ${SERVICE_NAME}"
[[ ${SERVICE_USER} =~ ^[A-Za-z_][A-Za-z0-9_-]*$ ]] || fail "Invalid service user: ${SERVICE_USER}"
[[ ${SECURE_COOKIE} == "true" || ${SECURE_COOKIE} == "false" ]] || fail "GOWEBSSH_SECURE_COOKIE must be true or false."
[[ ${ALLOW_PRIVATE_RANGES} == "true" || ${ALLOW_PRIVATE_RANGES} == "false" ]] || fail "GOWEBSSH_ALLOW_PRIVATE_RANGES must be true or false."
for yaml_value in "${LISTEN}" "${WEB_USERNAME}" "${GOWEBSSH_PASSWORD:-}"; do
  [[ ${yaml_value} != *$'\n'* && ${yaml_value} != *'"'* ]] || fail "Installer values cannot contain quotes or newlines."
done

for command in curl tar sha256sum install systemctl getent useradd; do
  command -v "${command}" >/dev/null 2>&1 || fail "Required command not found: ${command}"
done

case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) fail "Unsupported architecture: $(uname -m). Supported: amd64, arm64." ;;
esac

CURL_ARGS=(--fail --silent --show-error --location --retry 3)
if [[ -n ${TOKEN} ]]; then
  CURL_ARGS+=(--header "Authorization: Bearer ${TOKEN}")
fi

if [[ ${VERSION} == "latest" ]]; then
  log "Resolving the latest release..."
  release_json="$(curl "${CURL_ARGS[@]}" \
    --header "Accept: application/vnd.github+json" \
    --header "X-GitHub-Api-Version: 2022-11-28" \
    "https://api.github.com/repos/${REPOSITORY}/releases/latest")" || \
    fail "Unable to query the latest release. If the API is rate-limited or the repo requires auth, export GITHUB_TOKEN first."
  VERSION="$(printf '%s' "${release_json}" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
  [[ -n ${VERSION} ]] || fail "The latest release response did not contain a tag name."
fi

archive="go-webssh_${VERSION}_linux_${ARCH}.tar.gz"
base_url="https://github.com/${REPOSITORY}/releases/download/${VERSION}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

log "Downloading ${archive}..."
curl "${CURL_ARGS[@]}" --output "${tmp_dir}/${archive}" "${base_url}/${archive}" || \
  fail "Unable to download ${archive}. Check the version and repository permissions."
curl "${CURL_ARGS[@]}" --output "${tmp_dir}/SHA256SUMS" "${base_url}/SHA256SUMS" || \
  fail "Unable to download SHA256SUMS."

checksum_line="$(grep -E "[[:space:]](\./)?${archive}$" "${tmp_dir}/SHA256SUMS" | head -n 1 || true)"
[[ -n ${checksum_line} ]] || fail "No checksum was published for ${archive}."
expected_checksum="${checksum_line%%[[:space:]]*}"
actual_checksum="$(sha256sum "${tmp_dir}/${archive}" | awk '{print $1}')"
[[ ${actual_checksum} == "${expected_checksum}" ]] || fail "SHA256 verification failed."
log "SHA256 verification passed."

mkdir -p "${tmp_dir}/unpacked"
tar -xzf "${tmp_dir}/${archive}" --strip-components=1 -C "${tmp_dir}/unpacked"
[[ -x "${tmp_dir}/unpacked/go-webssh" ]] || fail "Release archive does not contain go-webssh."

if ! getent passwd "${SERVICE_USER}" >/dev/null; then
  nologin_shell="$(command -v nologin || true)"
  [[ -n ${nologin_shell} ]] || nologin_shell="/usr/sbin/nologin"
  useradd --system --home-dir "${DATA_DIR}" --shell "${nologin_shell}" "${SERVICE_USER}"
fi

install -d -m 0755 "${INSTALL_DIR}" "${CONFIG_DIR}"
install -d -m 0750 -o "${SERVICE_USER}" -g "${SERVICE_USER}" "${DATA_DIR}"
install -m 0755 "${tmp_dir}/unpacked/go-webssh" "${INSTALL_DIR}/go-webssh"

generated_password=""
config_file="${CONFIG_DIR}/config.yaml"
if [[ ! -f ${config_file} ]]; then
  random_hex() { od -An -N "$1" -tx1 /dev/urandom | tr -d ' \n'; }
  session_secret="$(random_hex 32)"
  generated_password="${GOWEBSSH_PASSWORD:-$(random_hex 12)}"

  cat >"${config_file}" <<EOF
server:
  listen: "${LISTEN}"
  session_secret: "${session_secret}"
  secure_cookie: ${SECURE_COOKIE}

auth:
  username: "${WEB_USERNAME}"
  password: "${generated_password}"
  session_ttl: "12h"

ssh:
  connect_timeout: "15s"
  idle_timeout: "30m"
  max_sessions: 5
  host_key_policy: "known-hosts"
  known_hosts_file: "${CONFIG_DIR}/known_hosts"

network_policy:
  allow_private_ranges: ${ALLOW_PRIVATE_RANGES}
  deny:
    - "127.0.0.0/8"
    - "169.254.0.0/16"
    - "::1/128"
    - "0.0.0.0/8"

logging:
  level: "info"

credentials:
  enabled: true
  db_file: "${DATA_DIR}/credentials.db"
  key_file: "${DATA_DIR}/credentials.db.key"

ui:
  themes_dir: "${DATA_DIR}/themes"
EOF
  chown root:"${SERVICE_USER}" "${config_file}"
  chmod 0640 "${config_file}"
  install -m 0644 -o "${SERVICE_USER}" -g "${SERVICE_USER}" /dev/null "${CONFIG_DIR}/known_hosts"
else
  log "Keeping existing configuration: ${config_file}"
fi

install -d -m 0750 -o "${SERVICE_USER}" -g "${SERVICE_USER}" "${DATA_DIR}/themes"

unit_file="/etc/systemd/system/${SERVICE_NAME}.service"
cat >"${unit_file}" <<EOF
[Unit]
Description=Go WebSSH lightweight browser SSH client
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
ExecStart=${INSTALL_DIR}/go-webssh --config ${config_file}
Restart=on-failure
RestartSec=3s
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=${CONFIG_DIR} ${DATA_DIR}
AmbientCapabilities=
CapabilityBoundingSet=

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now "${SERVICE_NAME}.service"
systemctl restart "${SERVICE_NAME}.service"

if ! systemctl is-active --quiet "${SERVICE_NAME}.service"; then
  systemctl --no-pager --full status "${SERVICE_NAME}.service" || true
  fail "Service failed to start."
fi

log "Installed Go WebSSH ${VERSION} (${ARCH})."
log "Service: systemctl status ${SERVICE_NAME}"
log "Config:  ${config_file}"
log "Listen:  http://${LISTEN}"
if [[ -n ${generated_password} ]]; then
  printf '\nInitial login credentials (shown once):\n'
  printf '  Username: %s\n' "${WEB_USERNAME}"
  printf '  Password: %s\n\n' "${generated_password}"
  printf 'Store this password securely, then replace auth.password with a bcrypt password_hash.\n'
fi
