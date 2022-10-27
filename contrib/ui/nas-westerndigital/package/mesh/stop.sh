#!/bin/sh

MESH_PACKAGE_LOG=/var/log/mesh.log
echo "stop.sh called" >> "$MESH_PACKAGE_LOG"

rm -f /usr/local/apache2/conf/extra/apache-mesh.conf
(/usr/sbin/apache restart web ) &

# ash is VERY limited, so use only basic ops
pid=`pidof -s mesh`
if [ -z "$pid" ]; then
  echo "stop.sh: mesh was not running" >> "$MESH_PACKAGE_LOG"
  exit 0
fi

echo "stop.sh: stop attempt #1" >> "$MESH_PACKAGE_LOG"
kill "$pid"
sleep 2

pid=`pidof -s mesh`
if [ -z "$pid" ]; then
  echo "stop.sh: stopped" >> "$MESH_PACKAGE_LOG"
  exit 0
fi

echo "stop.sh: stop attempt #2" >> "$MESH_PACKAGE_LOG"
kill "$pid"
sleep 4

pid=`pidof -s mesh`
if [ -z "$pid" ]; then
  echo "stop.sh: stopped" >> "$MESH_PACKAGE_LOG"
  exit 0
fi

echo "stop.sh: stop attempt #3" >> "$MESH_PACKAGE_LOG"
kill "$pid"
sleep 4

pid=`pidof -s mesh`
if [ -z "$pid" ]; then
  echo "stop.sh: stopped" >> "$MESH_PACKAGE_LOG"
  exit 0
fi

echo "stop.sh: hard kill" >> "$MESH_PACKAGE_LOG"
pkill -9 mesh
