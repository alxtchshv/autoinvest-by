@echo off
chcp 65001 > nul
title AutoInvest — тестовый стенд
cd /d "%~dp0"
rem Отдельная база data-test, тестовая платёжка, учебные покупки по реальным ценам.
rem test_stand.bat reset — стереть тестовую базу и начать заново.
set DATA_DIR=data-test
set PAYMENTS=sandbox
set BROKER=paper
set MARKET=live
set APP_ENV=development
set LAUNCH_MODE=open
set ADDR=:8092
set PUBLIC_URL=http://localhost:8092
set TELEGRAM_BOT_TOKEN=
set TELEGRAM_SUPPORT_CHAT_ID=
set GOOGLE_CLIENT_ID=
set GOOGLE_CLIENT_SECRET=
set OAUTH_GOOGLE=sandbox
set SMTP_HOST=
set BEPAID_SHOP_ID=
set BEPAID_SECRET_KEY=
set BEPAID_TEST=true

netstat -ano | findstr /r /c:":8092 .*LISTENING" >nul
if %errorlevel%==0 (
    echo Стенд уже запущен: http://localhost:8092 — просто откройте его в браузере.
    echo Чтобы перезапустить, закройте окно с работающим стендом и запустите этот файл снова.
    start http://localhost:8092
    pause
    exit /b 0
)

if /i "%~1"=="reset" (
    if exist data-test rmdir /s /q data-test
    echo Тестовая база удалена
)

where go >nul 2>nul
if %errorlevel%==0 (
    if not exist bin mkdir bin
    set CGO_ENABLED=0
    go build -trimpath -ldflags="-s -w" -o bin\autoinvest.exe .\cmd\autoinvest
    if errorlevel 1 (
        echo [ОШИБКА] Сборка не удалась. Установите Go 1.24+: https://go.dev/dl/
        pause
        exit /b 1
    )
)
if not exist "bin\autoinvest.exe" (
    echo [ОШИБКА] Нет bin\autoinvest.exe и не найден Go.
    pause
    exit /b 1
)

bin\autoinvest.exe demo
bin\autoinvest.exe admin admin@autoinvest.test admin12345

echo ================================================================
echo  Тестовый стенд: http://localhost:8092
echo  Пользователь:   demo@autoinvest.by / demo12345
echo  Админка:        admin@autoinvest.test / admin12345  (/admin)
echo  Тестовые карты: 4200 0000 0000 0000 — всё проходит
echo                  4005 5500 0000 0019 — списания отклоняются
echo  Инструкция:     ТЕСТИРОВАНИЕ_ВРУЧНУЮ.md
echo ================================================================
start "" cmd /c "timeout /t 2 /nobreak > nul & start http://localhost:8092"
bin\autoinvest.exe
pause
