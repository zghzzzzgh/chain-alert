@echo off
title chain-alert
cd /d "%~dp0"

set HTTPS_PROXY=http://127.0.0.1:7890
set HTTP_PROXY=http://127.0.0.1:7890

echo ==========================================
echo  chain-alert
echo  Config: config.yaml
echo ==========================================
echo.

REM --- check local proxy first ---
powershell -NoProfile -Command "$c=New-Object Net.Sockets.TcpClient; try{$c.Connect('127.0.0.1',7890); $c.Close(); exit 0}catch{exit 1}"
if errorlevel 1 (
  echo [ERROR] Proxy 127.0.0.1:7890 is NOT running.
  echo         Please turn on your VPN / proxy first.
  echo.
  pause
  exit /b 1
)
echo Proxy OK.
echo.

chain-alert.exe -config config.yaml

echo.
echo chain-alert exited.
pause
