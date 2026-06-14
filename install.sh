#!/bin/bash
set -e

APP_NAME="svgextract"
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
      apt-get update -qq
      apt-get install -y inkscape curl wget tar
      ;;
    dnf)
      dnf install -y inkscape curl wget tar
      ;;
    yum)
      yum install -y inkscape curl wget tar
      ;;
    *)
      echo "Package manager tidak dikenali. Install inkscape secara manual."
      exit 1
      ;;
  esac
}

install_go() {
  if command -v go &>/dev/null; then return; fi
  ARCH=$(uname -m)
  case $ARCH in
    x86_64) GOARCH="amd64" ;;
    aarch64|arm64) GOARCH="arm64" ;;
    armv7l) GOARCH="armv6l" ;;
    *) echo "Arsitektur $ARCH tidak didukung." ; exit 1 ;;
  esac
  GOVER="1.24.4"
  GOTAR="go$GOVER.linux-$GOARCH.tar.gz"
  wget -q "https://go.dev/dl/$GOTAR" -O /tmp/$GOTAR
  rm -rf /usr/local/go
  tar -C /usr/local -xzf /tmp/$GOTAR
  rm /tmp/$GOTAR
  export PATH=$PATH:/usr/local/go/bin
  echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile.d/go.sh
}

build_app() {
  export PATH=$PATH:/usr/local/go/bin
  mkdir -p $APP_DIR
  cp -r . $APP_DIR/src
  cd $APP_DIR/src
  go mod download
  CGO_ENABLED=0 go build -ldflags="-s -w" -o $BIN .
  cd -
  rm -rf $APP_DIR/src
}

create_service() {
  PORT="${PORT:-8090}"
  cat > $SERVICE_FILE <<EOF
[Unit]
Description=Generator Kartu Otomatis
After=network.target

[Service]
Type=simple
ExecStart=$BIN
Restart=on-failure
RestartSec=5
Environment=PORT=$PORT
WorkingDirectory=$APP_DIR
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable $APP_NAME
  systemctl restart $APP_NAME
}

echo "=== Instalasi $APP_NAME ==="
install_deps
install_go
build_app
create_service
echo "=== Selesai. Service berjalan di port ${PORT:-8090} ==="
echo "Cek status: systemctl status $APP_NAME"
