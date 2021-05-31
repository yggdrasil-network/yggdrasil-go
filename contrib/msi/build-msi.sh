#!/bin/bash

# This script generates an MSI file for Yggdrasil for a given architecture. It
# needs to run on Linux or macOS with Go 1.16, wixl and msitools installed.
#
# Author: Neil Alexander <neilalexander@users.noreply.github.com>

# Get arch from command line if given
PKGARCH=$1
if [ "${PKGARCH}" == "" ];
then
  echo "tell me the architecture: x86, x64 or arm"
  exit 1
fi

# Get the rest of the repository history. This is needed within Appveyor because
# otherwise we don't get all of the branch histories and therefore the semver
# scripts don't work properly.
if [ "${APPVEYOR_PULL_REQUEST_HEAD_REPO_BRANCH}" != "" ];
then
  git fetch --all
  git checkout ${APPVEYOR_PULL_REQUEST_HEAD_REPO_BRANCH}
elif [ "${APPVEYOR_REPO_BRANCH}" != "" ];
then
  git fetch --all
  git checkout ${APPVEYOR_REPO_BRANCH}
fi

# Build Yggdrasil!
[ "${PKGARCH}" == "x64" ] && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 ./build -p -l "-aslr"
[ "${PKGARCH}" == "x86" ] && GOOS=windows GOARCH=386 CGO_ENABLED=0 ./build -p -l "-aslr"
[ "${PKGARCH}" == "arm" ] && GOOS=windows GOARCH=arm CGO_ENABLED=0 ./build -p -l "-aslr"
#[ "${PKGARCH}" == "arm64" ] && GOOS=windows GOARCH=arm64 CGO_ENABLED=0 ./build

# Create the postinstall script
cat > updateconfig.bat << EOF
if not exist %ALLUSERSPROFILE%\\Yggdrasil (
  mkdir %ALLUSERSPROFILE%\\Yggdrasil
)
if not exist %ALLUSERSPROFILE%\\Yggdrasil\\yggdrasil.conf (
  if exist yggdrasil.exe (
    if not exist %ALLUSERSPROFILE%\\Yggdrasil\\yggdrasil.conf (
      yggdrasil.exe -genconf > %ALLUSERSPROFILE%\\Yggdrasil\\yggdrasil.conf
    )
  )
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
curl -o wintun.zip https://www.wintun.net/builds/wintun-0.10.2.zip
unzip wintun.zip
if [ $PKGARCH = "x64" ]; then
  PKGWINTUNDLL=wintun/bin/amd64/wintun.dll
elif [ $PKGARCH = "x86" ]; then
  PKGWINTUNDLL=wintun/bin/x86/wintun.dll
elif [ $PKGARCH = "arm" ]; then
  PKGWINTUNDLL=wintun/bin/arm/wintun.dll
#elif [ $PKGARCH = "arm64" ]; then
#  PKGWINTUNDLL=wintun/bin/arm64/wintun.dll
else
  echo "wasn't sure which architecture to get wintun for"
  exit 1
fi

if [ $PKGNAME != "master" ]; then
  PKGDISPLAYNAME="Yggdrasil Network (${PKGNAME} branch)"
else
  PKGDISPLAYNAME="Yggdrasil Network"
fi

# Generate the wix.xml file
cat > wix.xml << EOF
<?xml version="1.0" encoding="windows-1252"?>
<Wix xmlns="http://schemas.microsoft.com/wix/2006/wi">
  <Product
    Name="${PKGDISPLAYNAME}"
    Id="*"
    UpgradeCode="${PKGGUID}"
    Language="1033"
    Codepage="1252"
    Version="${PKGVERSIONMS}"
    Platform="${PKGARCH}"
    Manufacturer="github.com/yggdrasil-network">

    <Package
      Id="*"
      Keywords="Installer"
      Description="Yggdrasil Network Installer"
      Comments="Yggdrasil Network standalone router for Windows."
      Manufacturer="github.com/yggdrasil-network"
      InstallerVersion="200"
      InstallScope="perMachine"
      Languages="1033"
      Compressed="yes"
      Platform="${PKGARCH}"
      SummaryCodepage="1252" />

    <MajorUpgrade
      AllowDowngrades="yes" />

    <Media
      Id="1"
      Cabinet="Media.cab"
      EmbedCab="yes"
      CompressionLevel="high" />

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

            <File
              Id="Wintun"
              Name="wintun.dll"
              DiskId="1"
              Source="${PKGWINTUNDLL}" />

            <ServiceInstall
              Id="ServiceInstaller"
              Account="LocalSystem"
              Description="Yggdrasil Network router process"
              DisplayName="Yggdrasil Service"
              ErrorControl="normal"
              LoadOrderGroup="NetworkProvider"
              Name="Yggdrasil"
              Start="auto"
              Type="ownProcess"
              Arguments='-useconffile "%ALLUSERSPROFILE%\\Yggdrasil\\yggdrasil.conf" -logto "%ALLUSERSPROFILE%\\Yggdrasil\\yggdrasil.log"'
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
              Name="updateconfig.bat"
              DiskId="1"
              Source="updateconfig.bat"
              KeyPath="yes"/>
          </Component>
        </Directory>
      </Directory>
    </Directory>

    <Feature Id="YggdrasilFeature" Title="Yggdrasil" Level="1">
      <ComponentRef Id="MainExecutable" />
      <ComponentRef Id="CtrlExecutable" />
      <ComponentRef Id="ConfigScript" />
    </Feature>

    <CustomAction
      Id="UpdateGenerateConfig"
      Directory="YggdrasilInstallFolder"
      ExeCommand="cmd.exe /c updateconfig.bat"
      Execute="deferred"
      Return="check"
      Impersonate="yes" />

    <InstallExecuteSequence>
      <Custom
        Action="UpdateGenerateConfig"
        Before="StartServices" />
    </InstallExecuteSequence>

  </Product>
</Wix>
EOF

# Generate the MSI
wixl -v wix.xml -a ${PKGARCH} -o ${PKGNAME}-${PKGVERSION}-${PKGARCH}.msi
