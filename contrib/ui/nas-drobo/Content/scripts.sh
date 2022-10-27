#!/usr/bin/env sh

tmpdir="/tmp/DroboApps/mesh"
errorfile="${tmpdir}/error.txt"
base_dir="/mnt/DroboFS/Shares/DroboApps/mesh"
config_dir="$base_dir/config"
config_file="$config_dir/mesh.conf"
prog_dir="$(dirname "$(realpath "${0}")")"


_install() {
	if [ ! -f "${errorfile}" ]
	then
		mkdir -p "${tmpdir}"
		if [ ! -f "$config_file" ]; then
			echo 3 > "${errorfile}"
		fi
	fi
	ln -s $base_dir/var/log/mesh.log $base_dir/www/log
	# install apache 2.x
	/usr/bin/DroboApps.sh install_version apache 2
}

_uninstall() {
    pid=`pidof -s mesh`
    if [ -z "$pid" ]; then
       echo "mesh was not running"
    else
       kill "$pid"
    fi
}

_update() {
	/bin/sh "${prog_dir}/service.sh" stop

	cd "$base_dir"
	rm -rf $(ls | grep -v 'host_uid.txt\|var\|config')

	echo 'update successful' > "${prog_dir}/update.log"
}
