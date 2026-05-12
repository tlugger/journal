#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="/home/pi/blog"
REPO="tlugger/journal"
SERVICE_NAME="blog"

CURR_DIR="$(pwd)"

spin() {
  local pid=$1 msg=$2
  local frames=("⠋" "⠙" "⠹" "⠸" "⠼" "⠴" "⠦" "⠧" "⠇" "⠏")
  local i=0
  while kill -0 "$pid" 2>/dev/null; do
    printf "\r  %s %s" "${frames[$((i % 10))]}" "$msg"
    i=$((i + 1))
    sleep 0.1
  done
  wait "$pid" && printf "\r  ✅ %s\n" "$msg" || { printf "\r  ❌ %s\n" "$msg"; return 1; }
}

step() { echo ""; echo "── $1 ──"; }
ok()   { echo "  ✅ $1"; }
warn() { echo "  ⚠️  $1"; }
fail() { echo "  ❌ $1"; exit 1; }

# ── Banner ───────────────────────────────────────────────────────────

mkdir -p "$INSTALL_DIR"

if [ -f "$INSTALL_DIR/blog" ]; then
  echo ""
  echo "  📓 Blog Updater"
  echo "  ───────────────"
  echo "  Updating existing installation"
else
  echo ""
  echo "  📓 Blog Installer"
  echo "  ─────────────────"
  echo "  Markdown-from-vault blog for blog.tylerkno.ws"
fi
echo ""

# ── .env ────────────────────────────────────────────────────────────
#
# Bare minimum: only BLOG_VAULT_DIR is required by the binary. Everything
# else (BLOG_ADDR, BLOG_SITE_URL, RSS metadata) is optional. How the
# vault directory gets populated is up to you — cron `aws s3 sync`,
# rsync, manual scp, whatever.

if [ "$CURR_DIR" != "$INSTALL_DIR" ] && [ -f "$CURR_DIR/.env" ]; then
  step "Loading .env"
  cp "$CURR_DIR/.env" "$INSTALL_DIR/.env"
  chmod 600 "$INSTALL_DIR/.env"
  ok "Copied .env from current directory"
elif [ -f "$INSTALL_DIR/.env" ]; then
  ok "Using existing $INSTALL_DIR/.env"
else
  step "Writing placeholder .env"
  cat > "$INSTALL_DIR/.env" << 'EOF'
# Local mirror of the vault. The blog binary reads from here.
# Populate this directory however you like (cron + aws s3 sync, etc.).
BLOG_VAULT_DIR=/home/pi/blog/vault
EOF
  chmod 600 "$INSTALL_DIR/.env"
  ok "Created $INSTALL_DIR/.env"
fi

# ── Architecture ─────────────────────────────────────────────────────

step "Detecting system"
ARCH=$(uname -m)
case "$ARCH" in
  aarch64|arm64) GOARCH="arm64" ;;
  armv7l|armhf)  GOARCH="arm" ;;
  x86_64)        GOARCH="amd64" ;;
  *)             fail "Unsupported architecture: $ARCH" ;;
esac
ok "Architecture: $ARCH → linux/$GOARCH"

# Stop running services before we touch the binary
if systemctl is-active "$SERVICE_NAME" &>/dev/null; then
  systemctl stop "$SERVICE_NAME"
  ok "Stopped $SERVICE_NAME"
fi

# ── Local artifact override (dev path) ───────────────────────────────

SKIP_BINARY=0
if [ "$CURR_DIR" != "$INSTALL_DIR" ] && [ -f "$CURR_DIR/blog" ]; then
  step "Installing local binary"
  cp "$CURR_DIR/blog" "$INSTALL_DIR/blog"
  chmod +x "$INSTALL_DIR/blog"
  SKIP_BINARY=1
  ok "Installed from current directory"
fi

# ── Binary from release or source ─────────────────────────────────────
#
# Templates and static assets are embedded into the binary via //go:embed,
# so the binary alone is the whole deploy — no separate `cp` step.

if [ "$SKIP_BINARY" = "0" ]; then
  step "Getting blog binary"

  DOWNLOAD_URL=$(curl -sf "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null \
    | grep "browser_download_url.*linux.*${GOARCH}" \
    | head -1 \
    | cut -d '"' -f 4 || true)

  if [ -n "$DOWNLOAD_URL" ]; then
    echo "  📦 Found release binary"
    (curl -sfL -o "$INSTALL_DIR/blog" "$DOWNLOAD_URL") &
    spin $! "Downloading binary"
    chmod +x "$INSTALL_DIR/blog"
  else
    echo "  📦 No release found — building from source"

    command -v git &>/dev/null || fail "git is required to build from source"

    if ! command -v go &>/dev/null; then
      warn "Go not found — installing via apt"
      (sudo apt-get update -qq && sudo apt-get install -y -qq golang-go) &
      spin $! "Installing Go"
    fi
    ok "Go $(go version | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -1) found"

    TMPDIR=$(mktemp -d)
    trap "rm -rf $TMPDIR" EXIT
    (git clone --depth 1 "https://github.com/$REPO.git" "$TMPDIR/journal" 2>/dev/null) &
    spin $! "Cloning repository"

    VERSION=$(git -C "$TMPDIR/journal" describe --tags --always 2>/dev/null || echo "dev")

    (cd "$TMPDIR/journal" && go build -ldflags "-X main.version=$VERSION" -o "$INSTALL_DIR/blog" ./cmd/blog 2>&1) &
    spin $! "Building binary"
    chmod +x "$INSTALL_DIR/blog"
  fi
fi
ok "Binary installed to $INSTALL_DIR/blog"

# ── Vault dir ────────────────────────────────────────────────────────

mkdir -p "$INSTALL_DIR/vault"

# ── systemd: blog.service ───────────────────────────────────────────

step "Writing systemd unit"
cat > "/etc/systemd/system/${SERVICE_NAME}.service" << EOF
[Unit]
Description=Blog server (markdown from Obsidian vault)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$INSTALL_DIR
EnvironmentFile=$INSTALL_DIR/.env
ExecStart=$INSTALL_DIR/blog
Restart=always
RestartSec=10
StandardOutput=append:$INSTALL_DIR/blog.log
StandardError=append:$INSTALL_DIR/blog.log

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE_NAME" >/dev/null 2>&1
systemctl restart "$SERVICE_NAME"
ok "Service enabled and started"

# ── Done ────────────────────────────────────────────────────────────

echo ""
echo "  📓 Blog is live!"
echo ""
echo "  One manual step the installer doesn't touch — add this to your Caddyfile:"
echo ""
echo "    blog.tylerkno.ws {"
echo "        reverse_proxy localhost:8106"
echo "    }"
echo ""
echo "  Then: sudo systemctl reload caddy"
echo ""
echo "  Useful commands:"
echo "    sudo nano $INSTALL_DIR/.env             # add optional vars"
echo "    sudo systemctl status $SERVICE_NAME     # check health"
echo "    sudo systemctl restart $SERVICE_NAME    # pick up .env changes"
echo "    tail -f $INSTALL_DIR/blog.log"
echo ""
