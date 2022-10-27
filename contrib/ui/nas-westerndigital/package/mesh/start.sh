#!/bin/sh

MESH_PACKAGE_LOG=/var/log/mesh.log
echo "start.sh called" >> "$MESH_PACKAGE_LOG"

pid=`pidof -s mesh`
if [ -n "$pid" ]; then
  echo "start.sh: mesh already running" >> "$MESH_PACKAGE_LOG"
  exit 0
fi

EXE_PATH=`readlink -f /usr/bin/mesh`
BIN_PATH=`dirname $EXE_PATH`
INSTALL_PATH=`dirname $BIN_PATH`
config_file=$INSTALL_PATH/mesh.conf

ln -fs "$INSTALL_PATH"/apache-mesh.conf /usr/local/apache2/conf/extra
(/usr/sbin/apache restart web ) &

START_COMMAND="/usr/bin/mesh -useconffile '$config_file' -httpaddress 'http://localhost:19019' -wwwroot '${INSTALL_PATH}/www' -logto '${INSTALL_PATH}/var/log/mesh.log'"
echo "start.sh: starting (START_COMMAND=$START_COMMAND)" >> "$MESH_PACKAGE_LOG"


(/usr/bin/mesh -useconffile "$config_file" -httpaddress "http://localhost:19019" -wwwroot "${INSTALL_PATH}/www" -logto "${INSTALL_PATH}/var/log/mesh.log") &
