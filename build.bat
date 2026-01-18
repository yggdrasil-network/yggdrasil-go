@echo off
setlocal enabledelayedexpansion

set PKGSRC=github.com/yggdrasil-network/yggdrasil-go/src/version

set LDFLAGS=-X %PKGSRC%.buildName=%PKGNAME% -X %PKGSRC%.buildVersion=%PKGVER%
set ARGS=-v

:parse_args
if "%1"=="" goto end_args
if "%1"=="-u" set UPX=true
if "%1"=="-t" set TABLES=true
if "%1"=="-d" set ARGS=%ARGS% -tags debug & set DEBUG=true
if "%1"=="-r" set ARGS=%ARGS% -race
if "%1"=="-p" set ARGS=%ARGS% -buildmode=pie
if "%1"=="-c" (
  shift
  set GCFLAGS=%GCFLAGS% %1
)
if "%1"=="-l" (
  shift
  set LDFLAGS=%LDFLAGS% %1
)
if "%1"=="-o" (
  shift
  set ARGS=%ARGS% -o %1
)
shift
goto parse_args

:end_args
if "%TABLES%"=="" if "%DEBUG%"=="" (
  set LDFLAGS=%LDFLAGS% -s -w
)

for %%C in (yggdrasil yggdrasilctl) do (
  echo Building: %%C.exe
  go build %ARGS% -ldflags="%LDFLAGS%" -gcflags="%GCFLAGS%" ./cmd/%%C

  if "%UPX%"=="true" (
    upx --brute %%C.exe
  )
)