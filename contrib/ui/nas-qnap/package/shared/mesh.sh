#!/bin/sh
QPKG_CONF="/etc/config/qpkg.conf"
CONF="/etc/config/mesh.conf"
QPKG_NAME="mesh"
QPKG_DIR=$(/sbin/getcfg $QPKG_NAME Install_Path -f $QPKG_CONF)
KERNEL_MODULES+=" tun"

load_kernel_modules(){
          local KERNEL_VERSION=$(/bin/uname -r)
          local KERNEL_MODULES_PATH="/lib/modules"
          for M in ${KERNEL_MODULES}; do
           if [ -f ${KERNEL_MODULES_PATH}/vpn/${M}.ko ]; then
                /sbin/insmod ${KERNEL_MODULES_PATH}/vpn/${M}.ko
                continue
           fi
           if [ -f ${KERNEL_MODULES_PATH}/qvpn/${M}.ko ]; then
                /sbin/insmod ${KERNEL_MODULES_PATH}/qvpn/${M}.ko
                continue
           fi
           if [ -f ${KERNEL_MODULES_PATH}/misc/${M}.ko ]; then
                /sbin/insmod ${KERNEL_MODULES_PATH}/misc/${M}.ko
                continue
           fi
           if [ -f ${KERNEL_MODULES_PATH}/others/${M}.ko ]; then
                /sbin/insmod ${KERNEL_MODULES_PATH}/others/${M}.ko
                continue
           fi
           if [ -f ${KERNEL_MODULES_PATH}/${KERNEL_VERSION}/${M}.ko ]; then
                /sbin/insmod ${KERNEL_MODULES_PATH}/${KERNEL_VERSION}/${M}.ko
                continue
           fi
          done
}

create_tun(){
    if ( [ ! -c /dev/net/tun ] ); then
      if ( [ ! -d /dev/net ] ); then
      mkdir -m 755 /dev/net
    fi
      mknod /dev/net/tun c 10 200
      chmod 0755 /dev/net/tun
    fi

    # Load the tun module if not already loaded
    if ( !(lsmod | grep -q "^tun\s") ); then
      insmod /lib/modules/tun.ko
    fi
}

start_service ()
{
    exec 2>>/tmp/mesh.log
    set -x

    #enable ipv6    
    sysctl -w net.ipv6.conf.all.disable_ipv6=0
    sysctl -w net.ipv6.conf.default.disable_ipv6=0

    # Create the necessary file structure for /dev/net/tun
    create_tun
    load_kernel_modules

    #. /etc/init.d/vpn_common.sh && load_kernel_modules

    if [ ! -f '/etc/config/apache/extra/apache-mesh.conf' ] ; then
      ln -sf $QPKG_DIR/apache-mesh.conf /etc/config/apache/extra/
      apache_reload=1
    fi    
    
    if ! grep '/etc/config/apache/extra/apache-mesh.conf' /etc/config/apache/apache.conf ; then
      echo 'Include /etc/config/apache/extra/apache-mesh.conf' >> /etc/config/apache/apache.conf
      apache_reload=1
    fi

    if [ -n "$apache_reload" ] ; then
      /usr/local/apache/bin/apachectl -k graceful
    fi
    
    # Launch the mesh in the background.
    ${QPKG_DIR}/bin/mesh -useconffile "$CONF" \
    -httpaddress "http://127.0.0.1:19019" \
    -wwwroot "$QPKG_DIR/www" \
    -logto "$QPKG_DIR/var/log/mesh.log" &
    if [ $? -ne 0 ]; then
      echo "Starting $QPKG_NAME failed"
      exit 1
    fi
}

stop_service ()
{
    # Kill mesh
    pid=`pidof -s mesh`
    if [ -z "$pid" ]; then
      echo "mesh was not running"
      exit 0
    fi
    kill "$pid"
}

case "$1" in
  start)
  start_service
    ;;

  stop)
    stop_service
    ;;

  restart)
    $0 stop
    $0 start
    ;;

  *)
    echo "Usage: $0 {start|stop|restart}"
    exit 1
esac

exit 0
