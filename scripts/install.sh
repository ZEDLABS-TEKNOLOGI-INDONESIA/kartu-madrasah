#!/bin/bash
set -e

APP_NAME="kartu-madrasah"
APP_DIR="/opt/$APP_NAME"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"
BIN="$APP_DIR/$APP_NAME"

detect_pkg() {
  if command -v apt-get &>/dev/null; then echo "apt"
  elif command -v dnf &>/dev/null; then echo "dnf"
  elif command -v yum &>/dev/null; then echo "yum"
  else echo "unknown"; fi
}

install_deps() {
  PKG=$(detect_pkg)
  case $PKG in
    apt)
      apt-get update -qq 2>/dev/null || true
      apt-get install -y inkscape curl wget tar git 2>/dev/null
      ;;
    dnf) dnf install -y inkscape curl wget tar git ;;
    yum) yum install -y inkscape curl wget tar git ;;
    *)
      echo "Package manager tidak dikenali."
      exit 1
      ;;
  esac
}

install_go() {
  export PATH=/usr/local/go/bin:$PATH
  if command -v go &>/dev/null && [[ "$(go version 2>/dev/null)" == *"go1.2"* ]]; then
    return
  fi
  ARCH=$(uname -m)
  case $ARCH in
    x86_64)        GOARCH="amd64" ;;
    aarch64|arm64) GOARCH="arm64" ;;
    armv7l)        GOARCH="armv6l" ;;
    *) echo "Arsitektur $ARCH tidak didukung."; exit 1 ;;
  esac
  GOVER="1.24.4"
  GOTAR="go${GOVER}.linux-${GOARCH}.tar.gz"
  wget -q "https://go.dev/dl/$GOTAR" -O "/tmp/$GOTAR"
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "/tmp/$GOTAR"
  rm "/tmp/$GOTAR"
  export PATH=/usr/local/go/bin:$PATH
  echo 'export PATH=/usr/local/go/bin:$PATH' > /etc/profile.d/go.sh
  chmod +x /etc/profile.d/go.sh
}

build_app() {
  export PATH=/usr/local/go/bin:$PATH
  local SRC="$APP_DIR/src"
  rm -rf "$SRC"
  git clone https://github.com/zulfikriyahya/kartu-madrasah.git "$SRC"
  cd "$SRC"
  go mod download
  CGO_ENABLED=0 go build -ldflags="-s -w" -o "$BIN" .
  cd - > /dev/null
  rm -rf "$SRC"
}

create_service() {
  PORT="${PORT:-8090}"
  WORKERS="${WORKERS:-8}"
  cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=Generator Kartu Otomatis
After=network.target

[Service]
Type=simple
ExecStart=$BIN
Restart=on-failure
RestartSec=5
Environment=PORT=$PORT
Environment=MAX_WORKERS=$WORKERS
WorkingDirectory=$APP_DIR
StandardOutput=journal
StandardError=journal

# Batasi resource
LimitNOFILE=65536
LimitNPROC=512
MemoryMax=4G
CPUQuota=80%

# Keamanan
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=$APP_DIR /tmp

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable "$APP_NAME"
  systemctl restart "$APP_NAME"
}

echo "=== Instalasi $APP_NAME ==="
install_deps
install_go
build_app
create_service
echo "=== Selesai. Service berjalan di port ${PORT:-8090} ==="
echo "Cek status: systemctl status $APP_NAME"
