@echo off
rem Builds a deploy folder in dist\ : maiservice, config.json (holds the port)
rem and a PM2 ecosystem.config.js that works on Linux and Windows.
rem
rem   build-dist.bat                    asks: 1) linux  2) windows  3) both
rem   build-dist.bat windows            or: linux, all
rem   build-dist.bat windows -arch arm64 -port 3090 -cli
rem   build-dist.bat -os linux          the flags of: go run ./tools/dist
rem
rem Output: dist\maiservice-<os>-<arch>\  (config.json, uploads, outputs kept on rebuild)
setlocal
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
  echo Go is not installed or not on PATH.
  echo Install Go 1.22 or newer from https://go.dev/dl/ and open a new window.
  set "RC=1"
  goto :done
)

rem A first argument without a leading "-" is the target system.
set "OSARG="
set "FIRST=%~1"
if not "%FIRST%"=="" if not "%FIRST:~0,1%"=="-" (
  set "OSARG=-os %FIRST%"
  shift
)
set "REST="
:collect
if "%~1"=="" goto :run
set "REST=%REST% %1"
shift
goto :collect

:run
go run ./tools/dist %OSARG%%REST%
set "RC=%ERRORLEVEL%"
if "%RC%"=="0" (
  echo.
  echo Done. Copy the folder under dist\ to the server and run: pm2 start ecosystem.config.js
) else (
  echo.
  echo Build failed with exit code %RC%.
)

:done
rem Started without arguments (for example by double-click): keep the window open.
if "%FIRST%"=="" pause
exit /b %RC%
