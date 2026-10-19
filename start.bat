@echo off
chcp 65001 >nul
title chain-alert
cd /d "%~dp0"

REM 走本地代理（Telegram 必须）
set HTTPS_PROXY=http://127.0.0.1:7890
set HTTP_PROXY=http://127.0.0.1:7890

echo ============================================
echo  chain-alert 启动中...
echo  配置文件: config.yaml
echo  退出请按 Ctrl+C
echo ============================================
echo.

chain-alert.exe -config config.yaml

echo.
echo 程序已退出
pause