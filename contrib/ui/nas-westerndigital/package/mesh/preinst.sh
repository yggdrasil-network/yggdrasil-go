#!/bin/sh

# Write to a different log, because main log will be overwritten by install.sh
MESH_PACKAGE_LOG=/var/log/mesh-preinst.log
echo "preinst.sh called" >> "$MESH_PACKAGE_LOG"

path_dst="$1"
echo "path_dst=$path_dst" >> "$MESH_PACKAGE_LOG"


chown -R nobody:share "$path_dst"
