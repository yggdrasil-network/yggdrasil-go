@echo off
rem SPDX-License-Identifier: GPL-2.0
rem Copyright (C) 2019 WireGuard LLC. All Rights Reserved.

setlocal
setlocal EnableDelayedExpansion
set PATHEXT=.exe
set BUILDDIR=%~dp0
cd /d %BUILDDIR% || exit /b 1

set WIX_CANDLE_FLAGS=-nologo
set WIX_LIGHT_FLAGS=-nologo -spdb -sice:ICE71 -sice:ICE61

set WINTUN_VERSION=0.7
set WINTUN_AMD64_SHA256=c87f9e0df51ac5c4fc8551f38c58b6fe6c43c06344451e3ed8939f05f4979c7d
set WINTUN_X86_SHA256=bdc40a2314964759653cd0717983dd3fedfeb53e7e15f4ed8370e12964c04b43

if exist .deps\prepared goto :build
:installdeps
	rmdir /s /q .deps 2> NUL
	mkdir .deps || goto :error
	cd .deps || goto :error
	call :download wintun-x86.msm https://www.wintun.net/builds/wintun-x86-%WINTUN_VERSION%.msm %WINTUN_X86_SHA256% || goto :error
	call :download wintun-amd64.msm https://www.wintun.net/builds/wintun-amd64-%WINTUN_VERSION%.msm %WINTUN_AMD64_SHA256% || goto :error
	call :download wix-binaries.zip http://wixtoolset.org/downloads/v3.14.0.2812/wix314-binaries.zip 923892298f37514622c58cbbd9c2cadf2822d9bb53df8ee83aaeb05280777611 || goto :error
	echo [+] Extracting wix-binaries.zip
	mkdir wix\bin || goto :error
	unzip wix-binaries.zip -d wix\bin || goto :error
	echo [+] Cleaning up wix-binaries.zip
	del wix-binaries.zip || goto :error
	copy /y NUL prepared > NUL || goto :error
	cd .. || goto :error

:build
	set WIX=%BUILDDIR%.deps\wix\
	call :msi x86 i686 x86 || goto :error
	call :msi amd64 x86_64 x64 || goto :error
	if exist ..\sign.bat call ..\sign.bat
	if "%SigningCertificate%"=="" goto :success
	if "%TimestampServer%"=="" goto :success
	echo [+] Signing
	signtool sign /sha1 "%SigningCertificate%" /fd sha256 /tr "%TimestampServer%" /td sha256 /d "Yggdrasil Setup" "yggdrasil-*.msi" || goto :error

:success
	echo [+] Success.
	exit /b 0

:download
	echo [+] Downloading %1
	curl -#fLo %1 %2 || exit /b 1
	echo [+] Verifying %1
	for /f %%a in ('CertUtil -hashfile %1 SHA256 ^| findstr /r "^[0-9a-f]*$"') do if not "%%a"=="%3" exit /b 1
	goto :eof

:msi
	if not exist "%~1" mkdir "%~1"
	echo [+] Compiling %1
	"%WIX%bin\candle" %WIX_CANDLE_FLAGS% -dYGGDRASIL_PLATFORM="%~1" -dYGGDRASIL_BUILDNAME="%~1" -dYGGDRASIL_BUILDVERSION="%~1" -out "%~1\yggdrasil.wixobj" -arch %3 yggdrasil.wxs || exit /b %errorlevel%
	echo [+] Linking %1
	"%WIX%bin\light" %WIX_LIGHT_FLAGS% -out "yggdrasil-%~1.msi" "%~1\yggdrasil.wixobj" || exit /b %errorlevel%
	goto :eof

:error
	echo [-] Failed with error #%errorlevel%.
	cmd /c exit %errorlevel%
