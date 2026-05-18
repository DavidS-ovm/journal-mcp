#!/usr/bin/env bash
# Build journal-mcp from the current checkout, install the binary and the
# systemd user unit into $HOME, then reload + (re)start the service.
# Idempotent: safe to re-run after every code change.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${HOME}/.local/bin"
UNIT_DIR="${HOME}/.config/systemd/user"
UNIT_NAME="journal-mcp.service"

command -v go >/dev/null || { echo "go not on PATH" >&2; exit 1; }
command -v systemctl >/dev/null || { echo "systemctl not on PATH" >&2; exit 1; }

mkdir -p "${BIN_DIR}" "${UNIT_DIR}"

echo "==> Building journal-mcp from ${REPO_DIR}"
# Build into a tempfile so an in-flight server keeps running until install(1)
# atomically replaces the binary. install also fixes perms in one go.
tmp_bin="$(mktemp)"
trap 'rm -f "${tmp_bin}"' EXIT
( cd "${REPO_DIR}" && go build -o "${tmp_bin}" . )
install -m 0755 "${tmp_bin}" "${BIN_DIR}/journal-mcp"

echo "==> Installing ${UNIT_NAME} into ${UNIT_DIR}"
install -m 0644 "${REPO_DIR}/${UNIT_NAME}" "${UNIT_DIR}/${UNIT_NAME}"

echo "==> Reloading user systemd"
systemctl --user daemon-reload

# enable --now if it's not enabled yet, else just restart to pick up the new
# binary + any unit changes. enable --now on an already-enabled unit is a
# no-op but a restart is what we actually want for a refresh.
if systemctl --user is-enabled --quiet "${UNIT_NAME}"; then
    echo "==> Restarting ${UNIT_NAME}"
    systemctl --user restart "${UNIT_NAME}"
else
    echo "==> Enabling + starting ${UNIT_NAME}"
    systemctl --user enable --now "${UNIT_NAME}"
fi

# Give the unit a beat to either come up or fail visibly.
sleep 1
echo
systemctl --user --no-pager --lines=10 status "${UNIT_NAME}" || true
