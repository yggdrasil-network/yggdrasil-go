#!/bin/sh

# This script generates an MSI file for Mesh for a given architecture. It
# needs to run on Windows within MSYS2 and Go 1.13 or later must be installed on
# the system and within the PATH. This is ran currently by Appveyor or GitHub Actions (see
# appveyor.yml in the repository root) for both x86 and x64.
#
# Author: Neil Alexander <neilalexander@users.noreply.github.com>, Vadym Vikulin <vadym.vikulin@rivchain.org>

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

# Install prerequisites within MSYS2
pacman -S --needed --noconfirm unzip git curl

# Download the wix tools!
if [ ! -d wixbin ];
then
  curl -LO https://wixtoolset.org/downloads/v3.14.0.3910/wix314-binaries.zip
  if [ `md5sum wix314-binaries.zip | cut -f 1 -d " "` != "34f655cf108086838dd5a76d4318063b" ];
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

# Work out metadata for the package info
PKGNAME=$(sh contrib/semver/name.sh)
PKGVERSION=$(sh contrib/msi/msversion.sh --bare)
PKGVERSIONMS=$(echo $PKGVERSION | tr - .)
PKGINDEXFILE=contrib/ui/mesh-ui/index.html
PKGLICENSEFILE=LICENSE.rtf

#Build winres
go-winres simply --icon riv.ico --file-version $PKGVERSION --file-description "RiV-mesh (c) service, 2021 RIV CHAIN" \
--product-version $PKGVERSION --product-name "RiV-mesh" --copyright "Copyright (c) 2021, RIV CHAIN"
cp *.syso cmd/mesh
go-winres simply --icon riv.ico --file-version $PKGVERSION --file-description "RiV-mesh (c) GUI, 2021 RIV CHAIN" \
--product-version $PKGVERSION --product-name "RiV-mesh" --copyright "Copyright (c) 2021, RIV CHAIN" --manifest gui
cp *.syso contrib/ui/mesh-ui
go-winres simply --file-version $PKGVERSION --file-description "RiV-mesh (c) CLI, 2021 RIV CHAIN" \
--product-version $PKGVERSION --product-name "RiV-mesh" --copyright "Copyright (c) 2021, RIV CHAIN" --manifest cli
cp *.syso cmd/meshctl

# Build Mesh!
[ "${PKGARCH}" == "x64" ] && GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ LDFLAGS="-H windowsgui" ./build
[ "${PKGARCH}" == "x86" ] && GOOS=windows GOARCH=386 CGO_ENABLED=1 CC=i686-w64-mingw32-gcc CXX=i686-w64-mingw32-g++ LDFLAGS="-H windowsgui" ./build
[ "${PKGARCH}" == "arm" ] && GOOS=windows GOARCH=arm CGO_ENABLED=0 ./build

#[ "${PKGARCH}" == "x64" ] && GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ ./build
#[ "${PKGARCH}" == "x86" ] && GOOS=windows GOARCH=386 CGO_ENABLED=1 CC=i686-w64-mingw32-gcc CXX=i686-w64-mingw32-g++ ./build
#[ "${PKGARCH}" == "arm" ] && GOOS=windows GOARCH=arm CGO_ENABLED=0 ./build
#[ "${PKGARCH}" == "arm64" ] && GOOS=windows GOARCH=arm64 CGO_ENABLED=0 ./build

# Create the postinstall script
cat > updateconfig.bat << EOF
if not exist %ALLUSERSPROFILE%\\RiV-mesh (
  mkdir %ALLUSERSPROFILE%\\RiV-mesh
)
if not exist %ALLUSERSPROFILE%\\RiV-mesh\\mesh.conf (
  if exist mesh.exe (
    mesh.exe -genconf > %ALLUSERSPROFILE%\\RiV-mesh\\mesh.conf
  )
)
EOF

[ "${PKGARCH}" == "x64" ] && \
  PKGGUID="5bcfdddd-66a7-4eb7-b5f7-4a7500dcc65d" PKGINSTFOLDER="ProgramFiles64Folder" || \
  PKGGUID="cbf6ffa1-219e-4bb2-a0e5-74dbf1b58a45" PKGINSTFOLDER="ProgramFilesFolder"

# Download the Wintun driver
if [ ! -d wintun ];
then
  curl -o wintun.zip https://www.wintun.net/builds/wintun-0.14.1.zip
  unzip wintun.zip
fi
if [ $PKGARCH = "x64" ]; then
  PKGWINTUNDLL=wintun/bin/amd64/wintun.dll
  PKGWEBVIEWFILE=contrib/ui/mesh-ui/dll/x64/webview.dll
  PKGWEBVIEWFILELOADER=contrib/ui/mesh-ui/dll/x64/WebView2Loader.dll
elif [ $PKGARCH = "x86" ]; then
  PKGWINTUNDLL=wintun/bin/x86/wintun.dll
  PKGWEBVIEWFILE=contrib/ui/mesh-ui/dll/x86/webview.dll
  PKGWEBVIEWFILELOADER=contrib/ui/mesh-ui/dll/x86/WebView2Loader.dll
elif [ $PKGARCH = "arm" ]; then
  PKGWINTUNDLL=wintun/bin/arm/wintun.dll
#elif [ $PKGARCH = "arm64" ]; then
#  PKGWINTUNDLL=wintun/bin/arm64/wintun.dll
else
  echo "wasn't sure which architecture to get wintun for"
  exit 1
fi

if [ $PKGNAME != "master" ]; then
  PKGDISPLAYNAME="RiV-mesh Network (${PKGNAME} branch)"
else
  PKGDISPLAYNAME="RiV-mesh Network"
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
    Manufacturer="RiV-chain">

    <Package
      Id="*"
      Keywords="Installer"
      Description="RiV-mesh Network Installer"
      Comments="RiV-mesh Network standalone router for Windows."
      Manufacturer="RiV-chain"
      InstallerVersion="200"
      InstallScope="perMachine"
      Languages="1033"
      Compressed="yes"
      SummaryCodepage="1252" />

    <MajorUpgrade
      AllowDowngrades="yes" />

    <Media
      Id="1"
      Cabinet="Media.cab"
      EmbedCab="yes"
      CompressionLevel="high" />

    <Directory Id="TARGETDIR" Name="SourceDir">
      <Directory Id="DesktopFolder"  SourceName="Desktop"/>
      <Directory Id="${PKGINSTFOLDER}" Name="PFiles">
        <Directory Id="MeshInstallFolder" Name="RiV-mesh">
          <Component Id="MainExecutable" Guid="c2119231-2aa3-4962-867a-9759c87beb24">
            <File
              Id="Mesh"
              Name="mesh.exe"
              DiskId="1"
              Source="mesh.exe"
              KeyPath="yes" />

            <File
              Id="Wintun"
              Name="wintun.dll"
              DiskId="1"
              Source="${PKGWINTUNDLL}" />

            <ServiceInstall
              Id="MeshServiceInstaller"
              Account="LocalSystem"
              Description="RiV-mesh Network router process"
              DisplayName="RiV-mesh Service"
              ErrorControl="normal"
              LoadOrderGroup="NetworkProvider"
              Name="Mesh"
              Start="auto"
              Type="ownProcess"
              Arguments='-useconffile "%ALLUSERSPROFILE%\\RiV-mesh\\mesh.conf" -logto "%ALLUSERSPROFILE%\\RiV-mesh\\mesh.log"'
              Vital="yes" />

            <ServiceControl
              Id="MeshServiceControl"
              Name="Mesh"
              Start="install"
              Stop="both"
              Remove="uninstall" />
          </Component>

          <Component Id="CtrlExecutable" Guid="a916b730-974d-42a1-b687-d9d504cbb86a">
            <File
              Id="Meshctl"
              Name="meshctl.exe"
              DiskId="1"
              Source="meshctl.exe"
              KeyPath="yes"/>
          </Component>

          <Component Id="UIExecutable" Guid="ef9f30e0-8274-4526-835b-51bc09b5b1b7">

            <File
              Id="MeshUI"
              Name="mesh-ui.exe"
              DiskId="1"
              Source="mesh-ui.exe"
              KeyPath="yes" />

            <File
              Id="WebViewHtmlFile"
              Name="index.html"
              DiskId="1"
              Source="${PKGINDEXFILE}" />

            <File
              Id="WebViewDllFile"
              Name="webview.dll"
              DiskId="1"
              Source="${PKGWEBVIEWFILE}" />

            <File
              Id="WebViewLoaderFile"
              Name="WebView2Loader.dll"
              DiskId="1"
              Source="${PKGWEBVIEWFILELOADER}" />

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

    <Feature Id="MeshFeature" Title="Mesh" Level="1">
      <ComponentRef Id="MainExecutable" />
      <ComponentRef Id="UIExecutable" />
      <ComponentRef Id="CtrlExecutable" />
      <ComponentRef Id="cmpDesktopShortcut" />
      <ComponentRef Id="ConfigScript" />
    </Feature>

    <CustomAction
      Id="UpdateGenerateConfig"
      Directory="MeshInstallFolder"
      ExeCommand="cmd.exe /c updateconfig.bat"
      Execute="deferred"
      Return="check"
      Impersonate="yes" />

    <!-- Step 2: Add UI to your installer / Step 4: Trigger the custom action -->
    <UI>
        <UIRef Id="WixUI_Minimal" />
        <Publish Dialog="ExitDialog"
            Control="Finish"
            Event="DoAction"
            Value="LaunchApplication">WIXUI_EXITDIALOGOPTIONALCHECKBOX = 1 and NOT Installed</Publish>
    </UI>
    <WixVariable Id="WixUILicenseRtf" Value="${PKGLICENSEFILE}" />
    <Property Id="WIXUI_EXITDIALOGOPTIONALCHECKBOXTEXT" Value="Launch RiV-mesh" />

    <!-- Step 3: Include the custom action -->
    <Property Id="WixShellExecTarget" Value="[#MeshUI]" />
    <Property Id="ASSISTANCE_START_VIA_REGISTRY">1</Property>
    <CustomAction Id="LaunchApplication"
        BinaryKey="WixCA"
        DllEntry="WixShellExec"
        Impersonate="yes" />

    <InstallExecuteSequence>
      <Custom
        Action="UpdateGenerateConfig"
        Before="StartServices">
          NOT Installed AND NOT REMOVE
      </Custom>
    </InstallExecuteSequence>

    <Component Id="cmpDesktopShortcut" Guid="e32e4d07-abf8-4c37-a2c3-1ca4b4f98adc" Directory="DesktopFolder" >
        <Shortcut Id="RiVMeshDesktopShortcut"
             Name="RiV-mesh"
             Description="RiV-mesh is IoT E2E encrypted network"
             Directory="DesktopFolder"
             Target="[MeshInstallFolder]mesh-ui.exe"
             WorkingDirectory="MeshInstallFolder"/>
        <RegistryValue Root="HKCU" Key="Software\RiV-chain\RiV-mesh" Name="installed" Type="integer" Value="1" KeyPath="yes" />
        <RegistryValue Id="MerAs.rst" Root="HKMU" Action="write"
            Key="Software\Microsoft\Windows\CurrentVersion\Run"
            Name="RiV-mesh client"
            Value="[MeshInstallFolder]mesh-ui.exe"
            Type="string" />
        <Condition>ASSISTANCE_START_VIA_REGISTRY</Condition>
     </Component>

  </Product>
</Wix>
EOF

# Generate the MSI
CANDLEFLAGS="-nologo"
LIGHTFLAGS="-nologo -spdb -sice:ICE71 -sice:ICE61"
wixbin/candle $CANDLEFLAGS -out ${PKGNAME}-${PKGVERSION}-${PKGARCH}.wixobj -arch ${PKGARCH} wix.xml && \
wixbin/light $LIGHTFLAGS -ext WixUIExtension -ext WixUtilExtension -out ${PKGNAME}-${PKGVERSION}-${PKGARCH}.msi ${PKGNAME}-${PKGVERSION}-${PKGARCH}.wixobj
