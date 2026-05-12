#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="/home/pi/blog"
REPO="tlugger/journal"
SERVICE_NAME="blog"
SYNC_SERVICE_NAME="blog-vault-sync"

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
  IS_UPDATE=1
else
  echo ""
  echo "  📓 Blog Installer"
  echo "  ─────────────────"
  echo "  Markdown-from-vault blog for blog.tylerkno.ws"
  IS_UPDATE=0
fi
echo ""

# ── .env ────────────────────────────────────────────────────────────

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
# Required: where Obsidian's S3 sync lands. Use the s3:// URI of the
# directory that contains your vault root (the parent of blog/).
BLOG_S3_URI=s3://your-bucket/path/to/vault

# Local mirror of the vault. The blog binary reads from here.
BLOG_VAULT_DIR=/home/pi/blog/vault

# HTTP listen address; Caddy reverse-proxies to this.
BLOG_ADDR=:8106

# Canonical site URL (used in RSS link/guid).
BLOG_SITE_URL=https://blog.tylerkno.ws

# RSS author tag, optional. Format: "you@example.com (Display Name)"
BLOG_FEED_AUTHOR=

# AWS credentials for the sync. Use an IAM role if possible; otherwise:
AWS_REGION=us-east-1
# AWS_ACCESS_KEY_ID=
# AWS_SECRET_ACCESS_KEY=
EOF
  chmod 600 "$INSTALL_DIR/.env"
  ok "Created $INSTALL_DIR/.env with placeholders"
  warn "Edit .env before starting the service:"
  echo "     - BLOG_S3_URI must point to your vault root in S3"
  echo "     - AWS credentials must be present (env or IAM role)"
  NEEDS_CONFIG=1
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

# Also copy templates/static if we're running from a checkout.
if [ "$CURR_DIR" != "$INSTALL_DIR" ] && [ -d "$CURR_DIR/templates" ]; then
  step "Copying templates and static assets"
  cp -r "$CURR_DIR/templates" "$INSTALL_DIR/templates"
  cp -r "$CURR_DIR/static" "$INSTALL_DIR/static"
  ok "Templates + static copied from checkout"
  HAVE_ASSETS=1
fi

# ── Binary from release or source ─────────────────────────────────────

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

    if [ -z "${HAVE_ASSETS:-}" ]; then
      step "Installing templates and static assets"
      cp -r "$TMPDIR/journal/templates" "$INSTALL_DIR/templates"
      cp -r "$TMPDIR/journal/static" "$INSTALL_DIR/static"
      ok "Templates + static copied"
    fi
  fi
fi
ok "Binary installed to $INSTALL_DIR/blog"

# ── AWS CLI ──────────────────────────────────────────────────────────

if ! command -v aws &>/dev/null; then
  step "Installing AWS CLI"
  (sudo apt-get update -qq && sudo apt-get install -y -qq awscli) &
  spin $! "Installing awscli"
fi
AWS_BIN="$(command -v aws)"
ok "aws found at $AWS_BIN"

# ── Vault dir ────────────────────────────────────────────────────────

mkdir -p "$INSTALL_DIR/vault"

# ── systemd: blog.service ───────────────────────────────────────────

step "Writing systemd units"
cat > "/etc/systemd/system/${SERVICE_NAME}.service" << EOF
[Unit]
Description=Blog server (markdown from Obsidian vault)
After=network-online.target ${SYNC_SERVICE_NAME}.service
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$INSTALL_DIR
EnvironmentFile=$INSTALL_DIR/.env
Environment="BLOG_TEMPLATE_DIR=$INSTALL_DIR/templates"
Environment="BLOG_STATIC_DIR=$INSTALL_DIR/static"
ExecStart=$INSTALL_DIR/blog
Restart=always
RestartSec=10
StandardOutput=append:$INSTALL_DIR/blog.log
StandardError=append:$INSTALL_DIR/blog.log

[Install]
WantedBy=multi-user.target
EOF

# ── systemd: blog-vault-sync.service ────────────────────────────────

cat > "/etc/systemd/system/${SYNC_SERVICE_NAME}.service" << EOF
[Unit]
Description=Sync Obsidian vault from S3 to local mirror
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
WorkingDirectory=$INSTALL_DIR
EnvironmentFile=$INSTALL_DIR/.env
ExecStart=/bin/bash -c '$AWS_BIN s3 sync "\$BLOG_S3_URI" "\$BLOG_VAULT_DIR" --delete --exact-timestamps'
StandardOutput=append:$INSTALL_DIR/vault-sync.log
StandardError=append:$INSTALL_DIR/vault-sync.log
EOF

# ── systemd: blog-vault-sync.timer ──────────────────────────────────

cat > "/etc/systemd/system/${SYNC_SERVICE_NAME}.timer" << EOF
[Unit]
Description=Run vault sync every 5 minutes

[Timer]
OnBootSec=30s
OnUnitActiveSec=5min
Unit=${SYNC_SERVICE_NAME}.service
Persistent=true

[Install]
WantedBy=timers.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE_NAME" >/dev/null 2>&1
systemctl enable "${SYNC_SERVICE_NAME}.timer" >/dev/null 2>&1
ok "Units enabled"

if [ "${NEEDS_CONFIG:-}" = "1" ]; then
  warn "Services NOT started — fill in $INSTALL_DIR/.env first."
else
  systemctl start "${SYNC_SERVICE_NAME}.service" || warn "vault sync did not run cleanly — check $INSTALL_DIR/vault-sync.log"
  systemctl restart "$SERVICE_NAME"
  systemctl start "${SYNC_SERVICE_NAME}.timer"
  ok "Services started"
fi

# ── Done ────────────────────────────────────────────────────────────

echo ""
if [ "${NEEDS_CONFIG:-}" = "1" ]; then
  echo "  📓 Installed! Now finish setup:"
  echo ""
  echo "    1. sudo nano $INSTALL_DIR/.env        # set S3 URI + AWS creds"
  echo "    2. sudo systemctl start ${SYNC_SERVICE_NAME}.service ${SYNC_SERVICE_NAME}.timer"
  echo "    3. sudo systemctl start $SERVICE_NAME"
else
  echo "  📓 Blog is live!"
fi

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
echo "    sudo systemctl status $SERVICE_NAME"
echo "    sudo systemctl status ${SYNC_SERVICE_NAME}.timer"
echo "    sudo systemctl start ${SYNC_SERVICE_NAME}.service   # force a sync now"
echo "    tail -f $INSTALL_DIR/blog.log"
echo "    tail -f $INSTALL_DIR/vault-sync.log"
echo ""
