@echo off
chcp 65001 > nul
title AutoInvest
cd /d "%~dp0"
rem AutoInvest — настоящая версия до запуска: лист ожидания + демо-счёт. База data-real, ключи — из .env.
set DATA_DIR=data-real
set PAYMENTS=sandbox
set BROKER=paper
set MARKET=live
set APP_ENV=development
set LAUNCH_MODE=waitlist
set KYC_DOCS=manual
set MSI_MODE=off
set OAUTH_APPLE=off
set ADDR=:8090
set PUBLIC_URL=http://localhost:8090

netstat -ano | findstr /r /c:":8090 .*LISTENING" >nul
if %errorlevel%==0 (
    echo AutoInvest уже запущен: http://localhost:8090
    start http://localhost:8090
    pause
    exit /b 0
)

where go >nul 2>nul
if %errorlevel%==0 (
    if not exist bin mkdir bin
    set CGO_ENABLED=0
    go build -trimpath -ldflags="-s -w" -o bin\autoinvest.exe .\cmd\autoinvest
    if errorlevel 1 (
        echo [ОШИБКА] Сборка не удалась. Установите Go: https://go.dev/dl/
        pause
        exit /b 1
    )
)
if not exist "bin\autoinvest.exe" (
    echo [ОШИБКА] Нет bin\autoinvest.exe и не найден Go.
    pause
    exit /b 1
)

echo ================================================================
echo  AutoInvest (до запуска): http://localhost:8090
echo  Регистрация, анкета, копилка, лист ожидания и демо-счёт.
echo  Тестовый стенд с фейковыми картами: test_stand.bat (порт 8092)
echo ================================================================
start "" cmd /c "timeout /t 2 /nobreak > nul & start http://localhost:8090"
bin\autoinvest.exe
pause
