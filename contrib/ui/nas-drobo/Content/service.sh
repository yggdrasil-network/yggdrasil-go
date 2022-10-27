#!/bin/sh
#

##!!
. /etc/service.subr

prog_dir=`dirname \`realpath $0\``
base_dir=/mnt/DroboFS/Shares/DroboApps/mesh
config_dir="$base_dir/config"
config_file="$config_dir/mesh.conf"

name="mesh"
framework_version="2.1"
description="RiV-mesh is an implementation of a fully end-to-end encrypted IPv6 network"
depends=""
webui="WebUI"

errorfile=/tmp/DroboApps/mesh/error.txt
pidfile=/tmp/DroboApps/mesh/pid.txt
statusfile=/tmp/DroboApps/mesh/status.txt
edstatusfile=$base_dir/var/lib/mesh/status

start()
{
    mkdir -p /tmp/DroboApps/mesh
    # delete edstatufile before starting daemon to delete previous status
    rm -f $edstatusfile
    rm -f $errorfile

    if [ -f $config_file ]; then
       mkdir -p /var/backups
       echo "Backing up configuration file to /var/backups/mesh.conf.`date +%Y%m%d`"
       cp $config_file /var/backups/mesh.conf.`date +%Y%m%d`
       echo "Normalising and updating /etc/mesh.conf"
       $base_dir/bin/mesh -useconf -normaliseconf < /var/backups/mesh.conf.`date +%Y%m%d` > $config_file
    else
       mkdir -p $config_dir
       echo "Generating initial configuration file $config_file"
       echo "Please familiarise yourself with this file before starting RiV-mesh"
       sh -c "umask 0027 && $base_dir/bin/mesh -genconf > '$config_file'"
    fi

    # Create the necessary file structure for /dev/net/tun
    if ( [ ! -c /dev/net/tun ] ); then
      if ( [ ! -d /dev/net ] ); then
      mkdir -m 755 /dev/net
    fi
      mknod /dev/net/tun c 10 200
      chmod 0755 /dev/net/tun
    fi

    # Load the tun module if not already loaded
    if ( !(lsmod | grep -q "^tun\s") ); then
      KERNEL_VERSION=$(/bin/uname -r)
      insmod $base_dir/lib/modules/$KERNEL_VERSION/tun.ko
    fi

    # Launch the mesh in the background.
    ${base_dir}/bin/mesh -useconffile "$config_file" \
    -httpaddress "http://localhost:19019" \
    -wwwroot "$base_dir/www" \
    -logto "$base_dir/var/log/mesh.log" &    
    
    sleep 1
    update_status
}

update_status()
{

	# wait until file appears
	i=30

	while [ -z $(pidof -s mesh) ] 
	do
		sleep 1
		i=$((i-1))

		if [ $i -eq 0 ] 
		then
			break
		fi
	done

	# if we don't have file here. throw error into status and return
	if [ -z $(pidof -s mesh) ]
	then
		echo "" > "$pidfile"
		echo 1 > "${errorfile}"
		echo "Configuration required" > $statusfile
        else
        	echo $(pidof -s mesh) > "$pidfile"
        	echo 0 > "${errorfile}"
		echo "Application is running" > $statusfile
	fi

}

stop()
{
    pid=`pidof -s mesh`
    if [ -z "$pid" ]; then
       echo 1 > "${errorfile}"
       echo "mesh was not running" > $statusfile
    else
       kill "$pid"
       echo 0 > "${errorfile}"
       echo "Application is stopped" > $statusfile
    fi
    echo "" > "$pidfile"

}

case "$1" in
	update_status)
		update_status
		exit $?
		;;
esac

main "$@"
