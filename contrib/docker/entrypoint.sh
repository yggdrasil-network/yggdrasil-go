#!/usr/bin/env sh

set -e

CONF_DIR="/etc/yggdrasil-network"

if [ ! -f "$CONF_DIR/config.conf" ]; then
  echo "generate $CONF_DIR/config.conf"
  yggdrasil --genconf > "$CONF_DIR/config.conf"
fi

yggdrasil --useconf < "$CONF_DIR/config.conf"
exit $?
