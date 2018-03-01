#!/bin/sh

# This is a lazy script to create a .deb for Debian/Ubuntu. It installs
# yggdrasil and enables it in systemd. You can give it the PKGARCH= argument
# i.e. PKGARCH=i386 sh contrib/deb/generate.sh

if [ `pwd` != `git rev-parse --show-toplevel` ]
then
  echo "You should run this script from the top-level directory of the git repo"
  exit -1
fi

PKGNAME=debian-yggdrasil
PKGARCH=${PKGARCH-amd64}
PKGVERSION=0.$(git rev-list HEAD --count 2>/dev/null | xargs printf "%04d")
PKGFILE=$PKGNAME-$PKGVERSION-$PKGARCH.deb

echo "Building $PKGFILE"

mkdir -p /tmp/$PKGNAME/
mkdir -p /tmp/$PKGNAME/debian/
mkdir -p /tmp/$PKGNAME/usr/bin/
mkdir -p /tmp/$PKGNAME/etc/systemd/system/

cat > /tmp/$PKGNAME/debian/changelog << EOF
Insert changelog here
EOF
echo 9 > /tmp/$PKGNAME/debian/compat
cat > /tmp/$PKGNAME/debian/control << EOF
Package: $PKGNAME
Version: $PKGVERSION
Section: contrib/net
Priority: extra
Architecture: $PKGARCH
Maintainer: Neil Alexander <neilalexander@noreply.users.github.com>
Description: Debian yggdrasil package
 Binary yggdrasil package for Debian and Ubuntu
EOF
cat > /tmp/$PKGNAME/debian/copyright << EOF
Insert copyright notice here
EOF
cat > /tmp/$PKGNAME/debian/docs << EOF
Insert docs here
EOF
cat > /tmp/$PKGNAME/debian/install << EOF
usr/bin/yggdrasil usr/bin
etc/systemd/system/*.service etc/systemd/system
EOF
cat > /tmp/$PKGNAME/debian/postinst << EOF
#!/bin/sh
systemctl enable yggdrasil
systemctl start yggdrasil
EOF
cat > /tmp/$PKGNAME/debian/prerm << EOF
#!/bin/sh
systemctl disable yggdrasil
systemctl stop yggdrasil
EOF

if [ $PKGARCH = "amd64" ]; then GOARCH=amd64 GOOS=linux ./build; fi
if [ $PKGARCH = "i386" ]; then GOARCH=386 GOOS=linux ./build; fi

cp yggdrasil /tmp/$PKGNAME/usr/bin/
cp contrib/systemd/yggdrasil.service /tmp/$PKGNAME/etc/systemd/system/
cp contrib/systemd/yggdrasil-resume.service /tmp/$PKGNAME/etc/systemd/system/

tar -czvf /tmp/$PKGNAME/data.tar.gz -C /tmp/$PKGNAME/ \
  usr/bin/yggdrasil \
  etc/systemd/system/yggdrasil.service \
  etc/systemd/system/yggdrasil-resume.service
tar -czvf /tmp/$PKGNAME/control.tar.gz -C /tmp/$PKGNAME/debian .
echo 2.0 > /tmp/$PKGNAME/debian-binary

ar -r $PKGFILE \
  /tmp/$PKGNAME/debian-binary \
  /tmp/$PKGNAME/control.tar.gz \
  /tmp/$PKGNAME/data.tar.gz

rm -rf /tmp/$PKGNAME
