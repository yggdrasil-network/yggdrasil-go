#!/bin/sh

# This is a lazy script to create a .bin for WD NAS build.
# You can give it the PKGARCH= argument
# i.e. PKGARCH=x86_64 contrib/nas/nas-asustor.sh

if [ `pwd` != `git rev-parse --show-toplevel` ]
then
  echo "You should run this script from the top-level directory of the git repo"
  exit 1
fi

PKGBRANCH=$(basename `git name-rev --name-only HEAD`)
PKG=$(sh contrib/semmsiver/name.sh)
PKGVERSION=$(sh contrib/msi/msversion.sh --bare)
PKGARCH=${PKGARCH-amd64}
PKGFOLDER=$ENV_TAG-$PKGARCH-$PKGVERSION
PKGFILE=mesh-$PKGFOLDER.qpkg
PKGREPLACES=mesh

if [ $PKGBRANCH = "master" ]; then
  PKGREPLACES=mesh-develop
fi

if [ $PKGARCH = "x86-64" ]; then GOOS=linux GOARCH=amd64 ./build
elif [ $PKGARCH = "arm-x31" ]; then GOOS=linux GOARCH=arm GOARM=7 ./build
else
  echo "Specify PKGARCH=x86-64 or arm-x31"
  exit 1
fi

echo "Building $PKGFOLDER"

rm -rf /tmp/$PKGFOLDER

mkdir -p /tmp/$PKGFOLDER/mesh
mkdir -p /tmp/$PKGFOLDER/mesh/icons
mkdir -p /tmp/$PKGFOLDER/mesh/shared/bin
mkdir -p /tmp/$PKGFOLDER/mesh/shared/tmp
mkdir -p /tmp/$PKGFOLDER/mesh/shared/lib
mkdir -p /tmp/$PKGFOLDER/mesh/shared/www
mkdir -p /tmp/$PKGFOLDER/mesh/shared/var/log
chmod 0775 /tmp/$PKGFOLDER/ -R

echo "coping ui package..."
cp contrib/ui/nas-qnap/package/* /tmp/$PKGFOLDER/mesh -r
cp contrib/ui/nas-qnap/au/* /tmp/$PKGFOLDER/mesh/shared -r
cp contrib/ui/www/* /tmp/$PKGFOLDER/mesh/shared/www/ -r

echo "Converting icon for: 64x64"
convert -colorspace sRGB ./riv.png -resize 64x64 /tmp/$PKGFOLDER/mesh/icons/mesh.gif
echo "Converting icon for: 80x80"
convert -colorspace sRGB ./riv.png -resize 80x80 /tmp/$PKGFOLDER/mesh/icons/mesh_80.gif
convert -colorspace sRGB ./riv.png -resize 64x64 /tmp/$PKGFOLDER/mesh/icons/mesh_gray.gif

cat > /tmp/$PKGFOLDER/mesh/qpkg.cfg << EOF
QPKG_DISPLAY_NAME="RiV Mesh"
QPKG_NAME="mesh"
QPKG_VER="$PKGVERSION"
QPKG_AUTHOR="Riv Chain ltd"
QPKG_SUMMARY="RiV-mesh is an implementation of a fully end-to-end encrypted IPv6 network."
QPKG_RC_NUM="198"
QPKG_SERVICE_PROGRAM="mesh.sh"
QPKG_WEBUI="/mesh"
QPKG_WEB_PORT=
QPKG_LICENSE="LGPLv3"
QDK_BUILD_ARCH="$PKGARCH"
EOF

touch /tmp/$PKGFOLDER/mesh/qdk.conf

cp mesh /tmp/$PKGFOLDER/mesh/shared/bin
cp meshctl /tmp/$PKGFOLDER/mesh/shared/bin
chmod +x /tmp/$PKGFOLDER/mesh/shared/bin/*
chmod 0775 /tmp/$PKGFOLDER/mesh/shared/www -R
chmod -R u+rwX,go+rX,g-w /tmp/$PKGFOLDER

curent_dir=$(pwd)

cd /tmp/$PKGFOLDER/mesh && /opt/tomcat/tool/Qnap/bin/qbuild --force-config -v

mv build/*.qpkg "$curent_dir"/$PKGFILE

rm -rf /tmp/$PKGFOLDER/
