@echo off
chcp 65001 > nul
title AutoInvest BY — Создание Демо-аккаунта
cd /d "%~dp0"
if not exist "bin\autoinvest.exe" (
    echo Сначала запустите run.bat — он соберёт bin\autoinvest.exe
    pause
    exit /b 1
)
echo ========================================================
echo   👤 СОЗДАНИЕ ДЕМО-ПОЛЬЗОВАТЕЛЯ AUTOINVEST BY
echo ========================================================
echo.
bin\autoinvest.exe demo
echo.
echo ========================================================
echo Демо-вход: demo@autoinvest.by / demo12345
echo ========================================================
pause
