#!/bin/sh

# Check if xar and mkbom are available
command -v xar >/dev/null 2>&1 || (
  echo "Building xar"
  sudo apt-get install libxml2-dev libssl1.0-dev zlib1g-dev -y
  mkdir -p /tmp/xar && cd /tmp/xar
  git clone https://github.com/mackyle/xar && cd xar/xar
  (sh autogen.sh && make && sudo make install) || (echo "Failed to build xar"; exit 1)
)
command -v mkbom >/dev/null 2>&1 || (
  echo "Building mkbom"
  mkdir -p /tmp/mkbom && cd /tmp/mkbom
  git clone https://github.com/hogliux/bomutils && cd bomutils
  sudo make install || (echo "Failed to build mkbom"; exit 1)
)

# Build Yggdrasil
echo "running GO111MODULE=on GOOS=darwin GOARCH=${PKGARCH-amd64} ./build"
GO111MODULE=on GOOS=darwin GOARCH=${PKGARCH-amd64} ./build

# Check if we can find the files we need - they should
# exist if you are running this script from the root of
# the yggdrasil-go repo and you have ran ./build
test -f yggdrasil || (echo "yggdrasil binary not found"; exit 1)
test -f yggdrasilctl || (echo "yggdrasilctl binary not found"; exit 1)
test -f contrib/macos/yggdrasil.plist || (echo "contrib/macos/yggdrasil.plist not found"; exit 1)
test -f contrib/semver/version.sh || (echo "contrib/semver/version.sh not found"; exit 1)

# Delete the pkgbuild folder if it already exists
test -d pkgbuild && rm -rf pkgbuild

# Create our folder structure
mkdir -p pkgbuild/scripts
mkdir -p pkgbuild/flat/base.pkg
mkdir -p pkgbuild/flat/Resources/en.lproj
mkdir -p pkgbuild/root/usr/local/bin
mkdir -p pkgbuild/root/Library/LaunchDaemons

# Copy package contents into the pkgbuild root
cp yggdrasil pkgbuild/root/usr/local/bin
cp yggdrasilctl pkgbuild/root/usr/local/bin
cp contrib/macos/yggdrasil.plist pkgbuild/root/Library/LaunchDaemons

# Create the postinstall script
cat > pkgbuild/scripts/postinstall << EOF
#!/bin/sh

# Normalise the config if it exists, generate it if it doesn't
if [ -f /etc/yggdrasil.conf ];
then
  mkdir -p /Library/Preferences/Yggdrasil
  echo "Backing up configuration file to /Library/Preferences/Yggdrasil/yggdrasil.conf.`date +%Y%m%d`"
  cp /etc/yggdrasil.conf /Library/Preferences/Yggdrasil/yggdrasil.conf.`date +%Y%m%d`
  echo "Normalising /etc/yggdrasil.conf"
  /usr/local/bin/yggdrasil -useconffile /Library/Preferences/Yggdrasil/yggdrasil.conf.`date +%Y%m%d` -normaliseconf > /etc/yggdrasil.conf
else
  /usr/local/bin/yggdrasil -genconf > /etc/yggdrasil.conf
fi

# Unload existing Yggdrasil launchd service, if possible
test -f /Library/LaunchDaemons/yggdrasil.plist && (launchctl unload /Library/LaunchDaemons/yggdrasil.plist || true)

# Load Yggdrasil launchd service and start Yggdrasil
launchctl load /Library/LaunchDaemons/yggdrasil.plist
EOF

# Set execution permissions
chmod +x pkgbuild/scripts/postinstall
chmod +x pkgbuild/root/usr/local/bin/yggdrasil
chmod +x pkgbuild/root/usr/local/bin/yggdrasilctl

# Pack payload and scripts
( cd pkgbuild/scripts && find . | cpio -o --format odc --owner 0:80 | gzip -c ) > pkgbuild/flat/base.pkg/Scripts
( cd pkgbuild/root && find . | cpio -o --format odc --owner 0:80 | gzip -c ) > pkgbuild/flat/base.pkg/Payload

# Work out metadata for the package info
PKGNAME=$(sh contrib/semver/name.sh)
PKGVERSION=$(sh contrib/semver/version.sh --bare)
PKGARCH=${PKGARCH-amd64}
PAYLOADSIZE=$(( $(wc -c pkgbuild/flat/base.pkg/Payload | awk '{ print $1 }') / 1024 ))
[ "$PKGARCH" = "amd64" ] && PKGHOSTARCH="x86_64" || PKGHOSTARCH=${PKGARCH}

# Create the PackageInfo file
cat > pkgbuild/flat/base.pkg/PackageInfo << EOF
<pkg-info format-version="2" identifier="io.github.yggdrasil-network.pkg" version="${PKGVERSION}" install-location="/" auth="root">
  <payload installKBytes="${PAYLOADSIZE}" numberOfFiles="3"/>
  <scripts>
    <postinstall file="./postinstall"/>
  </scripts>
</pkg-info>
EOF

# Create the BOM
( cd pkgbuild && mkbom root flat/base.pkg/Bom )

# Create the Distribution file
cat > pkgbuild/flat/Distribution << EOF
<?xml version="1.0" encoding="utf-8"?>
<installer-script minSpecVersion="1.000000" authoringTool="com.apple.PackageMaker" authoringToolVersion="3.0.3" authoringToolBuild="174">
    <title>Yggdrasil (${PKGNAME}-${PKGVERSION})</title>
    <options customize="never" allow-external-scripts="no" hostArchitectures="${PKGHOSTARCH}" />
    <domains enable_anywhere="true"/>
    <installation-check script="pm_install_check();"/>
    <script>
    function pm_install_check() {
      if(!(system.compareVersions(system.version.ProductVersion,'10.10') >= 0)) {
        my.result.title = 'Failure';
        my.result.message = 'You need at least Mac OS X 10.10 to install Yggdrasil.';
        my.result.type = 'Fatal';
        return false;
      }
      return true;
    }
    </script>
    <choices-outline>
        <line choice="choice1"/>
    </choices-outline>
    <choice id="choice1" title="base">
        <pkg-ref id="io.github.yggdrasil-network.pkg"/>
    </choice>
    <pkg-ref id="io.github.yggdrasil-network.pkg" installKBytes="${PAYLOADSIZE}" version="${VERSION}" auth="Root">#base.pkg</pkg-ref>
</installer-script>
EOF

# Finally pack the .pkg
( cd pkgbuild/flat && xar --compression none -cf "../../${PKGNAME}-${PKGVERSION}-macos-${PKGARCH}.pkg" * )
