#!/bin/sh

# Get arch from command line if given
PKGARCH=$1
if [ "${PKGARCH}" == "" ];
then
  echo "tell me the architecture: x86 or x64"
  exit 1
fi

# Get the rest of the repository history
if [ "${APPVEYOR_REPO_BRANCH}" != "" ];
then
  git fetch --all
  git checkout ${APPVEYOR_REPO_BRANCH}
fi

# Install prerequisites
pacman -S --needed --noconfirm unzip git curl
# export PATH=$PATH:/c/go/bin/

# Download the wix tools!
if [ ! -d wixbin ];
then
  curl -LO https://github.com/wixtoolset/wix3/releases/download/wix3112rtm/wix311-binaries.zip
  if [ `md5sum wix311-binaries.zip | cut -f 1 -d " "` != "47a506f8ab6666ee3cc502fb07d0ee2a" ];
  then
    echo "wix package didn't match expected checksum"
    exit 1
  fi
  mkdir -p wixbin
  unzip -o wix311-binaries.zip -d wixbin || (
    echo "failed to unzip WiX"
    exit 1
  )
fi

# Check the prerequisite files are in place
(
  [ "${PKGARCH}" == "x64" ] && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 ./build
  [ "${PKGARCH}" == "x86" ] && GOOS=windows GOARCH=386 CGO_ENABLED=0 ./build
) || (
  echo "failed to build Yggdrasil"
  exit 1
)

# Create the postinstall script
cat > config.bat << EOF
if exist yggdrasil.conf (
  move yggdrasil.conf yggdrasil.conf.backup
  yggdrasil.exe -useconffile yggdrasil.conf.backup -normaliseconf > yggdrasil.conf
) else (
  yggdrasil.exe -genconf > yggdrasil.conf
)
EOF

# Work out metadata for the package info
PKGNAME=$(sh contrib/semver/name.sh)
PKGVERSION=$(sh contrib/semver/version.sh --bare)
PKGVERSIONMS=$(echo $PKGVERSION | tr - .)
[ "${PKGARCH}" == "x64" ] && \
  PKGGUID="77757838-1a23-40a5-a720-c3b43e0260cc" PKGINSTFOLDER="ProgramFiles64Folder" || \
  PKGGUID="54a3294e-a441-4322-aefb-3bb40dd022bb" PKGINSTFOLDER="ProgramFilesFolder"

# Download the Wintun driver
if [ $PKGARCH = "x64" ]; then
  PKGMSMNAME=wintun-x64.msm
  curl -o ${PKGMSMNAME} https://www.wintun.net/builds/wintun-amd64-0.7.msm || (echo "couldn't get wintun"; exit 1)
elif [ $PKGARCH = "x86" ]; then
  PKGMSMNAME=wintun-x86.msm
  curl -o ${PKGMSMNAME} https://www.wintun.net/builds/wintun-x86-0.7.msm || (echo "couldn't get wintun"; exit 1)
else
  echo "wasn't sure which architecture to get wintun for"
  exit 1
fi

# Generate the wix.xml file
cat > wix.xml << EOF
<?xml version="1.0" encoding="windows-1252"?>
<Wix xmlns="http://schemas.microsoft.com/wix/2006/wi">
  <Product
    Name="Yggdrasil (${PKGNAME} branch)"
    Id="${PKGGUID}"
    UpgradeCode="${PKGGUID}"
    Language="1033"
    Codepage="1252"
    Version="${PKGVERSIONMS}"
    Manufacturer="github.com/yggdrasil-network">

    <Package
      Id="*"
      Keywords="Installer"
      Description="Yggdrasil Network Installer"
      Comments="This is the Yggdrasil Network binary."
      Manufacturer="github.com/yggdrasil-network"
      InstallerVersion="200"
      Languages="1033"
      Compressed="yes"
      Platform="${PKGARCH}"
      SummaryCodepage="1252" />

    <MajorUpgrade
      AllowDowngrades="yes" />

    <Media
      Id="1"
      Cabinet="Media.cab"
      EmbedCab="yes" />

    <Directory Id="TARGETDIR" Name="SourceDir">
      <Directory Id="${PKGINSTFOLDER}" Name="PFiles">
        <Directory Id="YggdrasilInstallFolder" Name="Yggdrasil">

          <Component Id="MainExecutable" Guid="c2119231-2aa3-4962-867a-9759c87beb24">
            <File
              Id="Yggdrasil"
              Name="yggdrasil.exe"
              DiskId="1"
              Source="yggdrasil.exe"
              KeyPath="yes" />

            <ServiceInstall
              Id="ServiceInstaller"
              Account="LocalSystem"
              Description="Yggdrasil Network router process"
              DisplayName="Yggdrasil Service"
              ErrorControl="normal"
              LoadOrderGroup="NetworkProvider"
              Name="yggdrasil"
              Start="auto"
              Type="ownProcess"
              Arguments="-autoconf"
              Vital="yes" />

            <ServiceControl
              Id="ServiceControl"
              Name="yggdrasil"
              Start="install"
              Stop="both"
              Remove="uninstall" />
          </Component>

          <Component Id="CtrlExecutable" Guid="a916b730-974d-42a1-b687-d9d504cbb86a">
            <File
              Id="Yggdrasilctl"
              Name="yggdrasilctl.exe"
              DiskId="1"
              Source="yggdrasilctl.exe"
              KeyPath="yes"/>
          </Component>

          <Component Id="ConfigScript" Guid="64a3733b-c98a-4732-85f3-20cd7da1a785">
            <File
              Id="Configbat"
              Name="config.bat"
              DiskId="1"
              Source="config.bat"
              KeyPath="yes"/>
          </Component>
        </Directory>
      </Directory>

      <Merge Id="Wintun" Language="0" DiskId="1" SourceFile="${PKGMSMNAME}" />
    </Directory>

    <Feature Id="Complete" Level="1">
      <ComponentRef Id="MainExecutable" />
      <ComponentRef Id="CtrlExecutable" />
      <ComponentRef Id="ConfigScript" />
      <MergeRef Id="Wintun" />
    </Feature>

    <CustomAction
      Id="UpdateGenerateConfig"
      Directory="YggdrasilInstallFolder"
      ExeCommand="config.bat"
      Execute="commit"
      Return="asyncWait"/>

    <InstallExecuteSequence>
      <Custom
        Action="UpdateGenerateConfig"
        Before="MsiConfigureServices" />
    </InstallExecuteSequence>

  </Product>
</Wix>
EOF

# Generate the MSI
CANDLEFLAGS="-nologo"
LIGHTFLAGS="-nologo -spdb -sice:ICE71 -sice:ICE61"
wixbin/candle $CANDLEFLAGS -out ${PKGNAME}-${PKGVERSION}-${PKGARCH}.wixobj -arch ${PKGARCH} wix.xml && \
wixbin/light $LIGHTFLAGS -out ${PKGNAME}-${PKGVERSION}-${PKGARCH}.msi ${PKGNAME}-${PKGVERSION}-${PKGARCH}.wixobj
