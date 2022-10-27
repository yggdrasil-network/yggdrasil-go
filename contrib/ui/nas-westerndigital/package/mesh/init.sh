#!/bin/sh

install_path="$1"
config_file=$install_path/mesh.conf
backup_config_file=/var/backups/mesh.conf

MESH_PACKAGE_LOG=/var/log/mesh.log
echo "init.sh called" >> "$MESH_PACKAGE_LOG"

exec 2>>/var/log/mesh.log
set -x

mkdir -p /var/www/mesh

# Binaries
ln -s "$install_path/bin/mesh" /usr/bin
ln -s "$install_path/bin/meshctl" /usr/bin

# Web, probably, the app wil serve it by embedded server
ln -s "$install_path/www/mesh.png" /var/www/mesh
ln -s "$install_path/www/index.html" /var/www/mesh

if [ -f $backup_config_file ]; then
  echo "Backing up configuration file to /var/backups/mesh.conf"
  echo "Normalising and updating /etc/mesh.conf"
  mesh -useconf -normaliseconf < $backup_config_file > $config_file  
else
  echo "Generating initial configuration file $config_file"
  echo "Please familiarise yourself with this file before starting RiV-mesh"
  sh -c "umask 0027 && mesh -genconf > '$config_file'"
  mkdir -p /var/backups
fi

cp $config_file $backup_config_file
