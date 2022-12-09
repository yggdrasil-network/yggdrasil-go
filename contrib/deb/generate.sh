#!/bin/sh

# This is a lazy script to create a .deb for Debian/Ubuntu. It installs
# mesh and enables it in systemd. You can give it the PKGARCH= argument
# i.e. PKGARCH=i386 sh contrib/deb/generate.sh

if [ "$(pwd)" != "$(git rev-parse --show-toplevel)" ]
then
  echo "You should run this script from the top-level directory of the git repo"
  exit 1
fi

PKGBRANCH=$(basename "$(git name-rev --name-only HEAD)")
PKG=$(sh contrib/semver/name.sh)
PKGVERSION=$(sh contrib/semver/version.sh --bare)
PKGARCH=${PKGARCH-amd64}
PKGNAME=$PKG-$PKGVERSION-$PKGARCH-nogui
PKGFILE=$PKGNAME.deb
PKGREPLACES=mesh

if [ $PKGBRANCH = "master" ]; then
  PKGREPLACES=mesh-develop
fi

if [ $PKGARCH = "amd64" ]; then GOARCH=amd64 GOOS=linux ./build
elif [ $PKGARCH = "i386" ]; then GOARCH=386 GOOS=linux ./build
elif [ $PKGARCH = "mipsel" ]; then GOARCH=mipsle GOOS=linux ./build
elif [ $PKGARCH = "mips" ]; then GOARCH=mips64 GOOS=linux ./build
elif [ $PKGARCH = "armhf" ]; then GOARCH=arm GOOS=linux GOARM=6 ./build
elif [ $PKGARCH = "arm64" ]; then GOARCH=arm64 GOOS=linux ./build
elif [ $PKGARCH = "armel" ]; then GOARCH=arm GOOS=linux GOARM=5 ./build
else
  echo "Specify PKGARCH=amd64,i386,mips,mipsel,armhf,arm64,armel"
  exit 1
fi

echo "Building $PKGFILE"

mkdir -p /tmp/$PKGNAME/
mkdir -p /tmp/$PKGNAME/debian/
mkdir -p /tmp/$PKGNAME/DEBIAN/
mkdir -p /tmp/$PKGNAME/usr/bin/
mkdir -p /tmp/$PKGNAME/usr/local/bin/
mkdir -p /tmp/$PKGNAME/etc/systemd/system/
chmod 0775 /tmp/$PKGNAME/ -R

cat > /tmp/$PKGNAME/debian/changelog << EOF
Please see https://github.com/RiV-chain/RiV-mesh/
EOF
echo 9 > /tmp/$PKGNAME/debian/compat
cat > /tmp/$PKGNAME/DEBIAN/control << EOF
Package: mesh
Version: $PKGVERSION
Section: contrib/net
Priority: extra
Architecture: $PKGARCH
Replaces: $PKGREPLACES
Conflicts: $PKGREPLACES
Maintainer: Vadym Vikulin <vadym.vikulin@rivchain.org>
Description: RiV-mesh is an implementation of a fully end-to-end encrypted IPv6 network.
 It is lightweight, self-arranging, supported on multiple platforms and
 allows pretty much any IPv6-capable application to communicate securely with
 other RiV-mesh nodes.
EOF
cat > /tmp/$PKGNAME/debian/copyright << EOF
Please see https://github.com/RiV-chain/RiV-mesh/
EOF
cat > /tmp/$PKGNAME/debian/docs << EOF
Please see https://github.com/RiV-chain/RiV-mesh/
EOF
cat > /tmp/$PKGNAME/debian/install << EOF
usr/bin/mesh usr/bin
usr/bin/meshctl usr/bin
usr/local/bin/meshctl usr/local/bin
etc/systemd/system/*.service etc/systemd/system
EOF
cat > /tmp/$PKGNAME/DEBIAN/postinst << EOF
#!/bin/sh

if ! getent group mesh 2>&1 > /dev/null; then
  groupadd --system --force mesh || echo "Failed to create group 'mesh' - please create it manually and reinstall"
fi

if [ -f /etc/mesh.conf ]; then
  mkdir -p /var/backups
  echo "Backing up configuration file to /var/backups/mesh.conf.`date +%Y%m%d`"
  cp /etc/mesh.conf /var/backups/mesh.conf.`date +%Y%m%d`
  echo "Normalising and updating /etc/mesh.conf"
  /usr/bin/mesh -useconf -normaliseconf < /var/backups/mesh.conf.`date +%Y%m%d` > /etc/mesh.conf
else
  echo "Generating initial configuration file /etc/mesh.conf"
  echo "Please familiarise yourself with this file before starting RiV-mesh"
  sh -c 'umask 0027 && /usr/bin/mesh -genconf > /etc/mesh.conf'
fi
chgrp mesh /etc/mesh.conf
chmod 755 /etc/mesh.conf
if command -v systemctl >/dev/null; then
  systemctl daemon-reload || echo -n "daemon not reloaded!"
  systemctl enable mesh || echo -n "systemctl enable failed!"
  systemctl restart mesh || echo -n "systemctl restart failed!"
fi
EOF
cat > /tmp/$PKGNAME/DEBIAN/prerm << EOF
#!/bin/sh
if command -v systemctl >/dev/null; then
  if systemctl is-active --quiet mesh; then
    systemctl stop mesh || true
  fi
  systemctl disable mesh || true
fi
EOF

cp mesh /tmp/$PKGNAME/usr/bin/
cp meshctl /tmp/$PKGNAME/usr/bin/
ln -s /usr/bin/meshctl /tmp/$PKGNAME/usr/local/bin/meshctl
if [ "$LOGLEVEL" = "DEBUG" ]; then cp contrib/systemd/mesh-debug.service /tmp/$PKGNAME/etc/systemd/system/mesh.service
else
    cp contrib/systemd/mesh.service /tmp/$PKGNAME/etc/systemd/system/
fi

cp contrib/systemd/mesh-default-config.service /tmp/$PKGNAME/etc/systemd/system/
chmod 0775 /tmp/$PKGNAME/DEBIAN/*
chmod 644 /tmp/$PKGNAME/etc/systemd/system/*
chmod 755 /tmp/$PKGNAME/usr/bin/*

dpkg-deb --build --root-owner-group /tmp/$PKGNAME
cp /tmp/$PKGFILE .

rm -rf /tmp/$PKGNAME
