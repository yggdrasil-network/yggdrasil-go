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

# Build RiV-mesh
echo "running GO111MODULE=on GOOS=darwin GOARCH=${PKGARCH-amd64} ./build"
GO111MODULE=on GOOS=darwin GOARCH=${PKGARCH-amd64} ./build

# Check if we can find the files we need - they should
# exist if you are running this script from the root of
# the RiV-mesh repo and you have ran ./build
test -f mesh || (echo "mesh binary not found"; exit 1)
test -f meshctl || (echo "meshctl binary not found"; exit 1)
test -f mesh-ui || (echo "mesh-ui binary not found"; exit 1)
test -f contrib/macos/mesh.plist || (echo "contrib/macos/mesh.plist not found"; exit 1)
test -f contrib/semver/version.sh || (echo "contrib/semver/version.sh not found"; exit 1)

# Delete the pkgbuild folder if it already exists
test -d pkgbuild && rm -rf pkgbuild

# Create our folder structure

mkdir -p pkgbuild/scripts
mkdir -p pkgbuild/flat/base.pkg
mkdir -p pkgbuild/flat/Resources/en.lproj
mkdir -p pkgbuild/root/Applications/RiV-mesh.app/Contents/MacOS
mkdir -p pkgbuild/root/Applications/RiV-mesh.app/Contents/Resources
mkdir -p pkgbuild/root/usr/local/bin
mkdir -p pkgbuild/root/Library/LaunchDaemons

# Copy package contents into the pkgbuild root
cp meshctl pkgbuild/root/usr/local/bin
cp mesh pkgbuild/root/Applications/RiV-mesh.app/Contents/MacOS
cp mesh-ui pkgbuild/root/Applications/RiV-mesh.app/Contents/MacOS
cp riv.icns pkgbuild/root/Applications/RiV-mesh.app/Contents/Resources
cp -r contrib/ui/mesh-ui/ui pkgbuild/root/Applications/RiV-mesh.app/Contents/Resources
cp contrib/macos/mesh.plist pkgbuild/root/Library/LaunchDaemons

# Create open script
cat > pkgbuild/root/Applications/RiV-mesh.app/Contents/MacOS/open-mesh-ui << EOF
#!/usr/bin/env bash

exec /Applications/RiV-mesh.app/Contents/MacOS/mesh-ui 1>/tmp/mesh-ui.stdout.log 2>/tmp/mesh-ui.stderr.log
EOF

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
  /Applications/RiV-mesh.app/Contents/MacOS/mesh -useconffile /Library/Preferences/RiV-mesh/mesh.conf.`date +%Y%m%d` -normaliseconf > /etc/mesh.conf
else
  /Applications/RiV-mesh.app/Contents/MacOS/mesh -genconf > /etc/mesh.conf
fi

chmod 755 /etc/mesh.conf

# Unload existing RiV-mesh launchd service, if possible
test -f /Library/LaunchDaemons/mesh.plist && (launchctl unload /Library/LaunchDaemons/mesh.plist || true)

# Load RiV-mesh launchd service and start RiV-mesh
launchctl load /Library/LaunchDaemons/mesh.plist
EOF

# Set execution permissions
chmod 755 pkgbuild/scripts/postinstall
chmod 755 pkgbuild/root/usr/local/bin/meshctl
chmod 755 pkgbuild/root/Applications/RiV-mesh.app/Contents/MacOS/mesh
chmod 755 pkgbuild/root/Applications/RiV-mesh.app/Contents/MacOS/mesh-ui
chmod 755 pkgbuild/root/Applications/RiV-mesh.app/Contents/MacOS/open-mesh-ui

# Work out metadata for the package info
PKGNAME=$(sh contrib/semver/name.sh)
PKGVERSION=$(sh contrib/semver/version.sh --bare)
PKGARCH=${PKGARCH-amd64}

# Create the Info.plist file
cat > pkgbuild/root/Applications/RiV-mesh.app/Contents/Info.plist << EOF
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>org.riv-mesh.ui</string>
  <key>NSPrincipalClass</key>
  <string>NSApplication</string>
  <key>CFBundleName</key>
  <string>RiV-mesh</string>
  <key>NSHighResolutionCapable</key>
  <string>True</string>
  <key>CFBundleIconFile</key>
  <string>riv.icns</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleSignature</key>
  <string>????</string>
  <key>CFBundleGetInfoString</key>
  <string>${PKGVERSION}</string>
  <key>CFBundleVersion</key>
  <string>${PKGVERSION}</string>
  <key>CFBundleShortVersionString</key>
  <string>${PKGVERSION}</string>
  <key>CFBundleExecutable</key>
  <string>open-mesh-ui</string>
  <key>CFBundleIdentifier</key>
  <string>io.github.RiV-mesh.pkg</string>
</dict>
</plist>
EOF

# Pack payload and scripts
( cd pkgbuild/scripts && find . | cpio -o --format odc --owner 0:80 | gzip -c ) > pkgbuild/flat/base.pkg/Scripts
( cd pkgbuild/root && find . | cpio -o --format odc --owner 0:80 | gzip -c ) > pkgbuild/flat/base.pkg/Payload

PAYLOADSIZE=$(( $(wc -c pkgbuild/flat/base.pkg/Payload | awk '{ print $1 }') / 1024 ))

# Create the PackageInfo file
cat > pkgbuild/flat/base.pkg/PackageInfo << EOF
<pkg-info format-version="2" identifier="io.github.RiV-mesh.pkg" version="${PKGVERSION}" install-location="/" auth="root">
  <payload installKBytes="${PAYLOADSIZE}" numberOfFiles="6"/>
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
        <pkg-ref id="io.github.RiV-mesh.pkg"/>
    </choice>
    <pkg-ref id="io.github.RiV-mesh.pkg" installKBytes="${PAYLOADSIZE}" version="${VERSION}" auth="Root">#base.pkg</pkg-ref>
</installer-script>
EOF

# Finally pack the .pkg
( cd pkgbuild/flat && xar --compression none -cf "../../${PKGNAME}-${PKGVERSION}-macos-${PKGARCH}.pkg" * )
