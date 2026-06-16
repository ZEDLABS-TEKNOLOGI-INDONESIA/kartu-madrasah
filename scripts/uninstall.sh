#!/bin/bash
set -e

APP_NAME="kartu-madrasah"
APP_DIR="/opt/$APP_NAME"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"

if systemctl is-active --quiet $APP_NAME 2>/dev/null; then
  systemctl stop $APP_NAME
fi

if systemctl is-enabled --quiet $APP_NAME 2>/dev/null; then
  systemctl disable $APP_NAME
fi

[ -f "$SERVICE_FILE" ] && rm -f "$SERVICE_FILE"
systemctl daemon-reload

[ -d "$APP_DIR" ] && rm -rf "$APP_DIR"

echo "$APP_NAME berhasil di-uninstall."
