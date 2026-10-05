#!/bin/sh
# Install or upgrade mechon-agent on a Linux node.
#
#   curl -fsSL https://raw.githubusercontent.com/jub0t/mechon/main/scripts/install-agent.sh \
#     | sudo MECHON_PANEL_URL=https://panel.example.com MECHON_NODE_TOKEN=... sh
#
# Optional: MECHON_VERSION (e.g. v0.1.0, default: latest release), MECHON_DATA_DIR,
# MECHON_DOCKER_SOCKET, MECHON_DNS, MECHON_LOG_LEVEL.
#
# Re-running it upgrades the binary. /etc/mechon/agent.env keeps its values unless you pass new ones.
set -eu

REPO="jub0t/mechon"
BIN_PATH="/usr/local/bin/mechon-agent"
ENV_DIR="/etc/mechon"
ENV_FILE="$ENV_DIR/agent.env"
UNIT_FILE="/etc/systemd/system/mechon-agent.service"
# Variables written to agent.env when set.
ENV_VARS="MECHON_PANEL_URL MECHON_NODE_TOKEN MECHON_DATA_DIR MECHON_DOCKER_SOCKET MECHON_DNS MECHON_LOG_LEVEL"

info() { printf '==> %s\n' "$*"; }
fail() { printf 'error: %s\n' "$*" >&2; exit 1; }

# --- checks -------------------------------------------------------------------------------------

[ "$(uname -s)" = "Linux" ] || fail "mechon-agent runs on Linux only (this is $(uname -s))."
[ "$(id -u)" -eq 0 ] || fail "run this as root, e.g. 'curl -fsSL ... | sudo MECHON_PANEL_URL=... MECHON_NODE_TOKEN=... sh'."

case "$(uname -m)" in
	x86_64 | amd64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) fail "unsupported CPU architecture $(uname -m) (want x86_64 or aarch64)." ;;
esac

command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ] ||
	fail "systemd is required to run mechon-agent as a service."

if ! command -v docker >/dev/null 2>&1; then
	fail "Docker is not installed. Install Docker Engine first, then re-run this script:
    curl -fsSL https://get.docker.com | sh
  (or follow https://docs.docker.com/engine/install/ for your distribution)"
fi
if ! docker info >/dev/null 2>&1; then
	fail "Docker is installed but not running. Start it with:
    systemctl enable --now docker"
fi

missing=""
for tool in losetup mkfs.ext4 resize2fs iptables curl tar sha256sum; do
	command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
done
if [ -n "$missing" ]; then
	fail "missing required tools:$missing
  Debian/Ubuntu: apt-get install -y util-linux e2fsprogs iptables curl tar coreutils
  RHEL/Fedora:   dnf install -y util-linux e2fsprogs iptables curl tar coreutils"
fi

# --- configuration ------------------------------------------------------------------------------

# env_get NAME: print NAME's value from the existing agent.env, if any.
env_get() {
	[ -f "$ENV_FILE" ] || return 0
	sed -n "s/^$1=//p" "$ENV_FILE" | tail -n 1
}

for var in $ENV_VARS; do
	eval "new=\${$var:-}"
	if [ -z "$new" ]; then
		new="$(env_get "$var")"
	fi
	eval "$var=\$new"
done

[ -n "$MECHON_PANEL_URL" ] || fail "MECHON_PANEL_URL is required (the panel's public URL, e.g. https://panel.example.com)."
[ -n "$MECHON_NODE_TOKEN" ] || fail "MECHON_NODE_TOKEN is required (shown in the panel when you add a node)."
case "$MECHON_PANEL_URL" in
	http://* | https://*) ;;
	*) fail "MECHON_PANEL_URL must start with http:// or https:// (got '$MECHON_PANEL_URL')." ;;
esac

# --- download -----------------------------------------------------------------------------------

VERSION="${MECHON_VERSION:-}"
if [ -z "$VERSION" ]; then
	info "Finding the latest release"
	latest="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")" ||
		fail "could not reach github.com to find the latest release."
	VERSION="${latest##*/}"
	case "$VERSION" in
		v*) ;;
		*) fail "no published release found at https://github.com/$REPO/releases (set MECHON_VERSION to pick one)." ;;
	esac
fi
case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac
NUM="${VERSION#v}"
TARBALL="mechon-agent_${NUM}_linux_${ARCH}.tar.gz"
BASE="https://github.com/$REPO/releases/download/$VERSION"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
trap 'exit 1' INT TERM

info "Downloading mechon-agent $VERSION for linux/$ARCH"
curl -fsSL -o "$TMP/$TARBALL" "$BASE/$TARBALL" || fail "download failed: $BASE/$TARBALL"
curl -fsSL -o "$TMP/checksums.txt" "$BASE/checksums.txt" || fail "download failed: $BASE/checksums.txt"

info "Verifying the checksum"
expected="$(awk -v f="$TARBALL" '$2 == f || $2 == "*" f { print $1 }' "$TMP/checksums.txt")"
[ -n "$expected" ] || fail "$TARBALL is not listed in checksums.txt."
actual="$(sha256sum "$TMP/$TARBALL" | awk '{ print $1 }')"
[ "$expected" = "$actual" ] || fail "checksum mismatch for $TARBALL (expected $expected, got $actual)."

tar -xzf "$TMP/$TARBALL" -C "$TMP"
[ -f "$TMP/mechon-agent" ] || fail "$TARBALL does not contain mechon-agent."

# --- install ------------------------------------------------------------------------------------

info "Installing $BIN_PATH"
install -m 0755 "$TMP/mechon-agent" "$BIN_PATH.new"
mv -f "$BIN_PATH.new" "$BIN_PATH"

info "Writing $ENV_FILE"
mkdir -p "$ENV_DIR"
chmod 0755 "$ENV_DIR"
umask 077
{
	echo "# Written by install-agent.sh. Re-run the script with new values to change them."
	for var in $ENV_VARS; do
		eval "val=\${$var:-}"
		if [ -n "$val" ]; then
			printf '%s=%s\n' "$var" "$val"
		fi
	done
} >"$ENV_FILE.new"
chmod 0600 "$ENV_FILE.new"
mv -f "$ENV_FILE.new" "$ENV_FILE"

info "Writing $UNIT_FILE"
umask 022
cat >"$UNIT_FILE" <<EOF
[Unit]
Description=Mechon agent
Documentation=https://github.com/$REPO
After=docker.service network-online.target
Wants=network-online.target
Requires=docker.service

[Service]
EnvironmentFile=$ENV_FILE
ExecStart=$BIN_PATH
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable mechon-agent >/dev/null 2>&1
systemctl restart mechon-agent

sleep 2
if systemctl is-active --quiet mechon-agent; then
	info "mechon-agent $VERSION is running. The node should show as online in the panel shortly."
else
	fail "mechon-agent did not start. Check: journalctl -u mechon-agent -n 50"
fi
echo "    Logs:   journalctl -u mechon-agent -f"
echo "    Config: $ENV_FILE"
