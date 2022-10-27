#!/bin/sh

MESH_PACKAGE_LOG=/var/log/mesh.log
echo "remove.sh called" >> "$MESH_PACKAGE_LOG"
inst_path="$1"

rm -f /usr/bin/mesh
rm -f /usr/bin/meshctl
rm -fr /var/www/mesh
rm -fr "$inst_path"
