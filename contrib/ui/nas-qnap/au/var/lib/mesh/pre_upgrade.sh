#!/bin/sh

log_exit(){
	echo $2
	exit $1
}

[ -z "$MESH_USER_NAME" ] && log_exit 1 "Credentials are not set. Remove aborted"

rm "$MESH_APP_ROOT/bin/mesh
rm -rf "$MESH_APP_ROOT/www"

