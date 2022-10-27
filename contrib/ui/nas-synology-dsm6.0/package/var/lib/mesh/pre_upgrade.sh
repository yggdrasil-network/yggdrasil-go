#!/bin/sh

log_exit(){
	echo $2
	exit $1
}

rm "$ED_APP_ROOT/bin/mesh
rm -rf "$ED_APP_ROOT/www"
rm -rf "$ED_APP_ROOT/ui"
