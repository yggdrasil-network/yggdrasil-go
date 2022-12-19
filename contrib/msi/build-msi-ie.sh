#!/bin/sh

# This script generates an MSI file for Mesh for a given architecture. It
# needs to run on Windows within MSYS2 and Go 1.13 or later must be installed on
# the system and within the PATH. This is ran currently by Appveyor or GitHub Actions (see
# appveyor.yml in the repository root) for both x86 and x64.
#
# Author: Neil Alexander <neilalexander@users.noreply.github.com>

# Get arch from command line if given
PKGARCH=$1
if [ "${PKGARCH}" == "" ];
then
  echo "tell me the architecture: x86, x64, arm or arm64"
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
  curl -LO https://wixtoolset.org/downloads/v3.14.0.6526/wix314-binaries.zip
  if [ `md5sum wix314-binaries.zip | cut -f 1 -d " "` != "aecd655bb56238d48ef5254cd4dc958e" ];
  then
    echo "wix package didn't match expected checksum"
    exit 1
  fi
  mkdir -p wixbin
  unzip -o wix314-binaries.zip -d wixbin || (
    echo "failed to unzip WiX"
    exit 1
  )
fi

# Build Mesh!
[ "${PKGARCH}" == "x64" ] && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 ./build
[ "${PKGARCH}" == "x86" ] && GOOS=windows GOARCH=386 CGO_ENABLED=0 ./build
[ "${PKGARCH}" == "arm" ] && GOOS=windows GOARCH=arm CGO_ENABLED=0 ./build
[ "${PKGARCH}" == "arm64" ] && GOOS=windows GOARCH=arm64 CGO_ENABLED=0 ./build

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

# Work out metadata for the package info
PKGNAME=$(sh contrib/semver/name.sh)
PKGVERSION=$(sh contrib/msi/msversion.sh --bare)
PKGVERSIONMS=$(echo $PKGVERSION | tr - .)
PKGUIFOLDER=contrib/ui/mesh-ui/ui/

[ "${PKGARCH}" == "x64" ] && \
  PKGGUID="5bcfdddd-66a7-4eb7-b5f7-4a7500dcc65d" PKGINSTFOLDER="ProgramFiles64Folder" || \
  PKGGUID="cbf6ffa1-219e-4bb2-a0e5-74dbf1b58a45" PKGINSTFOLDER="ProgramFilesFolder"

PKGLICENSEFILE=LICENSE.rtf

# Download the Wintun driver
if [ ! -d wintun ];
then
  curl -o wintun.zip https://www.wintun.net/builds/wintun-0.14.1.zip
  unzip wintun.zip
fi

PKG_UI_ASSETS_ZIP=$(pwd)/ui.zip
( cd "$PKGUIFOLDER" && 7z a "$PKG_UI_ASSETS_ZIP" * )
PKG_UI_ASSETS_ZIP=ui.zip

if [ $PKGARCH = "x64" ]; then
  PKGWINTUNDLL=wintun/bin/amd64/wintun.dll
elif [ $PKGARCH = "x86" ]; then
  PKGWINTUNDLL=wintun/bin/x86/wintun.dll
elif [ $PKGARCH = "arm" ]; then
  PKGWINTUNDLL=wintun/bin/arm/wintun.dll
elif [ $PKGARCH = "arm64" ]; then
  PKGWINTUNDLL=wintun/bin/arm64/wintun.dll
else
  echo "wasn't sure which architecture to get wintun for"
  exit 1
fi

if [ $PKGNAME != "master" ]; then
  PKGDISPLAYNAME="RiV-mesh Network (${PKGNAME} branch)"
else
  PKGDISPLAYNAME="RiV-mesh Network"
fi

cat > mesh-ui-ie.js << EOF
var ie = new ActiveXObject("InternetExplorer.Application");
ie.AddressBar = false;
ie.MenuBar = false;
ie.ToolBar = false;
ie.height = 960
ie.width = 706
ie.resizable = false
ie.Visible = true;
ie.Navigate("http://localhost:19019");
EOF

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

    <Icon Id="icon.ico" SourceFile="riv.ico"/>
    <Property Id="ARPPRODUCTICON" Value="icon.ico" />
    <Property Id="WixShellExecTarget" Value="[#cscript.exe]" />

    <Directory Id="TARGETDIR" Name="SourceDir">
      <Directory Id="DesktopFolder"  SourceName="Desktop"/>
      <Directory Id="SystemFolder" Name="SystemFolder" />
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
              Id="IE_JS"
              Name="mesh-ui-ie.js"
              DiskId="1"
              Source="mesh-ui-ie.js" />
            <File
              Id="Wintun"
              Name="wintun.dll"
              DiskId="1"
              Source="${PKGWINTUNDLL}" />

            <ServiceInstall
              Id="ServiceInstaller"
              Account="LocalSystem"
              Description="RiV-mesh Network router process"
              DisplayName="RiV-mesh Service"
              ErrorControl="normal"
              LoadOrderGroup="NetworkProvider"
              Name="Mesh"
              Start="auto"
              Type="ownProcess"
              Arguments='-useconffile "%ALLUSERSPROFILE%\\RiV-mesh\\mesh.conf" -logto "%ALLUSERSPROFILE%\\RiV-mesh\\mesh.log" -httpaddress "http://localhost:19019" -wwwroot "[MeshInstallFolder]ui.zip"'
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
              Id="UiAssets"
              Name="ui.zip"
              DiskId="1"
              Source="${PKG_UI_ASSETS_ZIP}" />
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
    <CustomAction Id="LaunchApplication"
      BinaryKey="WixCA"
      DllEntry="WixShellExec"
      Impersonate="no"/>

    <!-- Step 3: Include the custom action -->
    <Property Id="ASSISTANCE_START_VIA_REGISTRY">1</Property>

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
             Target="[Script]"
             Arguments="[MeshInstallFolder]mesh-ui-ie.js"
             WorkingDirectory="MeshInstallFolder">
             <Icon Id="ShortcutIcon" SourceFile="riv.ico"/>
        </Shortcut>
        <RegistryValue Root="HKCU"
            Key="Software\RiV-chain\RiV-mesh"
            Name="installed"
            Type="integer"
            Value="1"
            KeyPath="yes" />
        <RegistryValue Id="MerAs.rst" Root="HKCU" Action="write"
            Key="Software\Microsoft\Windows\CurrentVersion\Run"
            Name="RiV-mesh client"
            Type="string"
            Value='"[Script]" "[MeshInstallFolder]mesh-ui-ie.js"' />
        <Condition>ASSISTANCE_START_VIA_REGISTRY</Condition>
     </Component>
  </Product>
</Wix>
EOF

# Generate the MSI
CANDLEFLAGS="-nologo"
LIGHTFLAGS="-nologo -spdb -sice:ICE71 -sice:ICE61"
wixbin/candle $CANDLEFLAGS -out ${PKGNAME}-${PKGVERSION}-${PKGARCH}.wixobj -arch ${PKGARCH} wix.xml && \
wixbin/light $LIGHTFLAGS -ext WixUIExtension -ext WixUtilExtension.dll -out ${PKGNAME}-${PKGVERSION}-${PKGARCH}-win7-ie.msi ${PKGNAME}-${PKGVERSION}-${PKGARCH}.wixobj
