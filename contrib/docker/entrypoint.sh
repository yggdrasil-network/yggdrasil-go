#!/usr/bin/env sh

set -e

CONF_DIR="/etc/RiV-chain"

if [ ! -f "$CONF_DIR/config.conf" ]; then
  echo "generate $CONF_DIR/config.conf"
  mesh --genconf > "$CONF_DIR/config.conf"
fi

mesh --useconf < "$CONF_DIR/config.conf"
exit $?
