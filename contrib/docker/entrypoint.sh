#!/usr/bin/env sh

set -e

CONF_DIR="/etc/yggdrasil-network"

if [ ! -f "$CONF_DIR/yggdrasil.conf" ]; then
  echo "generate $CONF_DIR/yggdrasil.conf"
  yggdrasil genconf -j > "$CONF_DIR/yggdrasil.conf"
fi

yggdrasil run --useconf -l trace < "$CONF_DIR/yggdrasil.conf"
exit $?
