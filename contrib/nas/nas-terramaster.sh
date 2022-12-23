#!/bin/sh

# This is a lazy script to create a .bin for Terramaster NAS build.
# You can give it the PKGARCH= argument
# i.e. PKGARCH=x86_64 contrib/nas/nas-terramaster.sh

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
PKGFILE=mesh-$PKGFOLDER.tpk
PKGREPLACES=mesh

if [ $PKGBRANCH = "master" ]; then
  PKGREPLACES=mesh-develop
fi

if [ -z $TERRAMASTER_TOOLS ]; then
  echo "Specify TERRAMASTER_TOOLS path"
  exit 1
fi

if [ $PKGARCH = "x86-64" ]; then GOOS=linux GOARCH=amd64 ./build
elif [ $PKGARCH = "arm-x31" ]; then GOOS=linux GOARCH=arm GOARM=7 ./build
else
  echo "Specify PKGARCH=x86-64 or arm-x31"
  exit 1
fi

echo "Building $PKGFOLDER"

rm -rf /tmp/$PKGFOLDER

mkdir -p /tmp/$PKGFOLDER/mesh/usr/bin
mkdir -p /tmp/$PKGFOLDER/mesh/var/log
mkdir -p /tmp/$PKGFOLDER/mesh/usr/www/images/icons
chmod 0775 /tmp/$PKGFOLDER/ -R

echo "coping ui package..."
cp contrib/ui/nas-terramaster/mesh /tmp/$PKGFOLDER/ -r
cp -r contrib/ui/mesh-ui/ui/* /tmp/$PKGFOLDER/mesh/usr/local/mesh/www

echo "Converting icon for: 120x120"
convert -colorspace sRGB ./riv.png -resize 120x120 PNG32:/tmp/$PKGFOLDER/mesh/usr/www/images/icons/mesh.png

echo "$PKGVERSION" > /tmp/$PKGFOLDER/mesh/version

cat > /tmp/$PKGFOLDER/mesh/mesh.lang << EOF
[zh-cn]
name = "RiV Mesh"
auth = "mesh"
version = "$PKGVERSION"
descript = "RiV-mesh is an implementation of a fully end-to-end encrypted IPv6 network."

[en-us]
name = "RiV Mesh"
auth = "mesh"
version = "$PKGVERSION"
descript = "RiV-mesh is an implementation of a fully end-to-end encrypted IPv6 network."
EOF

cp mesh /tmp/$PKGFOLDER/mesh/usr/bin
cp meshctl /tmp/$PKGFOLDER/mesh/usr/bin
cp riv.svg /tmp/$PKGFOLDER/mesh/usr/www/images/icons/mesh.svg
ln -s /usr/mesh/var/log/mesh.log /tmp/$PKGFOLDER/mesh/usr/local/mesh/www/log
chmod +x /tmp/$PKGFOLDER/mesh/usr/bin/*
chmod 0775 /tmp/$PKGFOLDER/mesh/usr/www -R
chmod -R u+rwX,go+rX,g-w /tmp/$PKGFOLDER

curent_dir=$(pwd)

cd /tmp/$PKGFOLDER/

cp $TERRAMASTER_TOOLS/makeapp .
cp $TERRAMASTER_TOOLS/install-* .
cp -r $TERRAMASTER_TOOLS/phpencode .

./makeapp mesh

cd dist/$PKGFOLDER && mv *.tpk "$curent_dir"/$PKGFILE

cd "$curent_dir"

rm -rf /tmp/$PKGFOLDER/
