#!/bin/sh

BASE="/usr/local/AppCentral/mesh-nas-asustor"
CONFIG_DIR="/usr/local/etc"

MESH_PACKAGE_LOG=/tmp/mesh.log
echo "start-stop called" >> "$MESH_PACKAGE_LOG"

exec 2>>$MESH_PACKAGE_LOG
set -x

whoami

init ()
{
    config_file=${CONFIG_DIR}/mesh.conf
    if [ ! -f "$CONFIG_DIR" ]; then
       mkdir -p ${CONFIG_DIR}
    fi
    
    if [ -f $config_file ]; then
       mkdir -p /var/backups
       echo "Backing up configuration file to /var/backups/mesh.conf.`date +%Y%m%d`"
       cp $config_file /var/backups/mesh.conf.`date +%Y%m%d`
       echo "Normalising and updating /etc/mesh.conf"
       ${BASE}/bin/mesh -useconf -normaliseconf < /var/backups/mesh.conf.`date +%Y%m%d` > $config_file
    else
       echo "Generating initial configuration file $config_file"
       echo "Please familiarise yourself with this file before starting RiV-mesh"
       sh -c "umask 0027 && ${BASE}/bin/mesh -genconf > '$config_file'"
    fi

    #chown -R admin:administrators $config_file
    #chmod -R 664 $config_file
    #sudo insmod /lib/modules/5.4.x/tun.ko
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
      insmod /lib/modules/5.4.x/tun.ko
    fi
}

start_service ()
{
    init

    # Launch the mesh in the background.
    ${BASE}/bin/mesh -useconffile "$config_file" \
    -httpaddress "http://0.0.0.0:19019" \
    -wwwroot "$BASE/www" \
    -logto "$BASE/var/log/mesh.log" &
    return $?
}

stop_service ()
{
    pid=`pidof -s mesh`
    if [ -z "$pid" ]; then
      echo "mesh was not running"
      exit 0
    fi
    kill "$pid"
}

case $1 in
    start)
        start_service
        echo "Running RiV Mesh"
        exit 0
        ;;
    stop)
        stop_service
        echo "Stopped RiV Mesh"
        exit 0
        ;;
    *)
        exit 1
        ;;
esac
