@echo off
chcp 65001 >nul

echo Starting mihomo...
cd /d "%~dp0clash"
start "" mihomo.exe -f config.yaml
cd /d "%~dp0"

echo Waiting for mihomo...
timeout /t 3 /nobreak >nul

echo Starting new-api...
set CLASH_API_URL=http://127.0.0.1:9090
set HTTP_PROXY=http://127.0.0.1:7890
set HTTPS_PROXY=http://127.0.0.1:7890

new-api.exe
