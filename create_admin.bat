@echo off
chcp 65001 > nul
title AutoInvest BY — Создание Администратора
cd /d "%~dp0"
if not exist "bin\autoinvest.exe" (
    echo Сначала запустите run.bat — он соберёт bin\autoinvest.exe
    pause
    exit /b 1
)
echo ========================================================
echo   👑 СОЗДАНИЕ АДМИНИСТРАТОРА AUTOINVEST BY
echo ========================================================
echo.
set /p ADMIN_EMAIL="Введите email администратора: "
set /p ADMIN_PASS="Введите пароль (мин. 8 симв, буквы и цифры): "
echo.
bin\autoinvest.exe admin %ADMIN_EMAIL% %ADMIN_PASS%
echo.
pause
