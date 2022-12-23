#!/bin/sh

# This is a lazy script to create a .deb for Debian/Ubuntu. It installs
# mesh and enables it in systemd. You can give it the PKGARCH= argument
# i.e. PKGARCH=i386 sh contrib/deb/generate.sh

if [ $(pwd) != $(git rev-parse --show-toplevel) ]
then
  echo "You should run this script from the top-level directory of the git repo"
  exit 1
fi

PKGBRANCH=$(basename `git name-rev --name-only HEAD`)
PKG=$(sh contrib/semver/name.sh)
PKGVERSION=$(sh contrib/semver/version.sh --bare)
PKGARCH=${PKGARCH-amd64}
PKGNAME=$ENV_TAG-$PKGVERSION
PKGFILE=$PKGNAME.deb
PKGREPLACES=mesh

if [ $PKGBRANCH = "master" ]; then
  PKGREPLACES=mesh-develop
fi

if [ $PKGARCH = "amd64" ]; then GOARCH=amd64 GOOS=linux ./build
elif [ $PKGARCH = "armel" ]; then GOARCH=arm GOARM=5 GOOS=linux ./build
else
  echo "Specify PKGARCH=amd64,armel"
  exit 1
fi

echo "Building $PKGFILE"

mkdir -p /tmp/$PKGNAME/usr/bin
mkdir -p /tmp/$PKGNAME/debian/
mkdir -p /tmp/$PKGNAME/DEBIAN/
mkdir -p /tmp/$PKGNAME/apps/mesh/bin
mkdir -p /tmp/$PKGNAME/apps/mesh/www
mkdir -p /tmp/$PKGNAME/apps/mesh/var/log
mkdir -p /tmp/$PKGNAME/apps/mesh/var/lib/mesh/hooks
mkdir -p /tmp/$PKGNAME/usr/share/doc/mesh

chmod 0775 /tmp/$PKGNAME/ -R

for resolution in 150x150; do
  echo "Converting icon for: $resolution"
  convert -colorspace sRGB ./riv.png -resize $resolution PNG32:/tmp/$PKGNAME/apps/mesh/logo.png  && \
  chmod 644 /tmp/$PKGNAME/apps/mesh/logo.png
done

cat > /tmp/$PKGNAME/apps/mesh/config.xml << EOF
<Application resource-id="mesh"><!-- 'resource-id' must be AppName -->
  <Name>RiV Mesh</Name><!-- Any desciptive name, upto 48 chars -->
  <Author>Riv Chain Ltd</Author><!-- Authors name. upto 48 chars -->
  <Version>$PKGVERSION</Version><!-- Version -->
  <RequireReboot>0</RequireReboot><!-- If non-zero, it indicates reboot is required. -->
  <ConfigURL></ConfigURL><!-- 'localhost' will be replaced by framework JS. -->
  <LaunchURL>https://localhost/apps/mesh/</LaunchURL><!-- 'localhost' will be replaced by framework JS. -->
  <ReservePort>19019</ReservePort>
  <DebianPackage>mesh</DebianPackage>
  <ServiceName>fvapp-mesh.service</ServiceName><!-- If start/stop need to start/stop service, specify service name -->
  <Description lang="en-us">RiV-mesh is an implementation of a fully end-to-end encrypted IPv6 network.</Description>
</Application>
EOF

echo "coping ui package..."
cp contrib/ui/nas-netgear-os6/package/apps /tmp/$PKGNAME/ -r
cp -r contrib/ui/mesh-ui/ui/* /tmp/$PKGNAME/apps/mesh/www/

echo "coping postinstall, postrm, prerm scripts"
cp contrib/ui/nas-netgear-os6/package/DEBIAN/* /tmp/$PKGNAME/DEBIAN/ -r

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

cp mesh /tmp/$PKGNAME/apps/mesh/bin
cp meshctl /tmp/$PKGNAME/apps/mesh/bin
ln -s /apps/mesh/bin/meshctl /tmp/$PKGNAME/usr/bin/meshctl
ln -s /apps/mesh/var/log/mesh.log /tmp/$PKGNAME/apps/mesh/www/log
chmod 0775 /tmp/$PKGNAME/DEBIAN/*
chmod 755 /tmp/$PKGNAME/apps/mesh/bin/*

dpkg-deb -Zxz --build --root-owner-group /tmp/$PKGNAME

cp /tmp/$PKGFILE .

rm -rf /tmp/$PKGNAME
