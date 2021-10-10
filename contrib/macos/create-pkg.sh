#!/bin/sh

# Check if xar and mkbom are available
command -v xar >/dev/null 2>&1 || (
  echo "Building xar"
  sudo apt-get install libxml2-dev libssl1.0-dev zlib1g-dev autoconf -y
  rm -rf /tmp/xar && mkdir -p /tmp/xar && cd /tmp/xar
  #git clone https://github.com/mackyle/xar && cd xar/xar
  git clone https://github.com/RiV-chain/xar.git && cd xar/xar
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
# the RiV-mesh repo and you have ran ./build
test -f mesh || (echo "mesh binary not found"; exit 1)
test -f meshctl || (echo "meshctl binary not found"; exit 1)
test -f contrib/macos/mesh.plist || (echo "contrib/macos/mesh.plist not found"; exit 1)
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
cp mesh pkgbuild/root/usr/local/bin
cp meshctl pkgbuild/root/usr/local/bin
cp contrib/macos/mesh.plist pkgbuild/root/Library/LaunchDaemons

# Create the postinstall script
cat > pkgbuild/scripts/postinstall << EOF
#!/bin/sh

# Normalise the config if it exists, generate it if it doesn't
if [ -f /etc/mesh.conf ];
then
  mkdir -p /Library/Preferences/RiV-mesh
  echo "Backing up configuration file to /Library/Preferences/RiV-mesh/mesh.conf.`date +%Y%m%d`"
  cp /etc/mesh.conf /Library/Preferences/RiV-mesh/mesh.conf.`date +%Y%m%d`
  echo "Normalising /etc/mesh.conf"
  /usr/local/bin/mesh -useconffile /Library/Preferences/RiV-mesh/mesh.conf.`date +%Y%m%d` -normaliseconf > /etc/mesh.conf
else
  /usr/local/bin/mesh -genconf > /etc/mesh.conf
fi

# Unload existing RiV-mesh launchd service, if possible
test -f /Library/LaunchDaemons/mesh.plist && (launchctl unload /Library/LaunchDaemons/mesh.plist || true)

# Load RiV-mesh launchd service and start RiV-mesh
launchctl load /Library/LaunchDaemons/mesh.plist
EOF

# Set execution permissions
chmod +x pkgbuild/scripts/postinstall
chmod +x pkgbuild/root/usr/local/bin/mesh
chmod +x pkgbuild/root/usr/local/bin/meshctl

# Pack payload and scripts
( cd pkgbuild/scripts && find . | cpio -o --format odc --owner 0:80 | gzip -c ) > pkgbuild/flat/base.pkg/Scripts
( cd pkgbuild/root && find . | cpio -o --format odc --owner 0:80 | gzip -c ) > pkgbuild/flat/base.pkg/Payload

# Work out metadata for the package info
PKGNAME=$(sh contrib/semver/name.sh)
PKGVERSION=$(sh contrib/semver/version.sh --bare)
PKGARCH=${PKGARCH-amd64}
PAYLOADSIZE=$(( $(wc -c pkgbuild/flat/base.pkg/Payload | awk '{ print $1 }') / 1024 ))

# Create the PackageInfo file
cat > pkgbuild/flat/base.pkg/PackageInfo << EOF
<pkg-info format-version="2" identifier="io.github.RiV-chain.pkg" version="${PKGVERSION}" install-location="/" auth="root">
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
    <title>RiV-mesh (${PKGNAME}-${PKGVERSION})</title>
    <options customize="never" allow-external-scripts="no"/>
    <domains enable_anywhere="true"/>
    <installation-check script="pm_install_check();"/>
    <script>
    function pm_install_check() {
      if(!(system.compareVersions(system.version.ProductVersion,'10.10') >= 0)) {
        my.result.title = 'Failure';
        my.result.message = 'You need at least Mac OS X 10.10 to install RiV-mesh.';
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
        <pkg-ref id="io.github.RiV-chain.pkg"/>
    </choice>
    <pkg-ref id="io.github.RiV-chain.pkg" installKBytes="${PAYLOADSIZE}" version="${VERSION}" auth="Root">#base.pkg</pkg-ref>
</installer-script>
EOF

# Finally pack the .pkg
( cd pkgbuild/flat && xar --compression none -cf "../../${PKGNAME}-${PKGVERSION}-macos-${PKGARCH}.pkg" * )
