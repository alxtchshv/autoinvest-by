@echo off
chcp 65001 > nul
title AutoInvest BY — Локальный сервер

echo ========================================================
echo        🚀 ЗАПУСК СЕРВИСА AUTOINVEST BY (v2.0)
echo ========================================================
echo.

cd /d "%~dp0"

where go >nul 2>nul
if %errorlevel%==0 (
    echo [1/2] Сборка bin\autoinvest.exe ^(повторная сборка занимает пару секунд^)...
    if not exist bin mkdir bin
    set CGO_ENABLED=0
    go build -trimpath -ldflags="-s -w" -o bin\autoinvest.exe .\cmd\autoinvest
    if errorlevel 1 (
        echo [ОШИБКА] Не удалось скомпилировать проект. Проверьте установку Go 1.24+.
        pause
        exit /b 1
    )
) else (
    if not exist "bin\autoinvest.exe" (
        echo [ОШИБКА] Go не найден и нет собранного bin\autoinvest.exe. Установите Go: https://go.dev/dl/
        pause
        exit /b 1
    )
    echo [1/2] Go не найден — запускаю уже собранный bin\autoinvest.exe
)

echo [2/2] Запуск сервера AutoInvest BY на http://localhost:8080...
echo.
echo ========================================================
echo 🌐 Платформа доступна по адресу: http://localhost:8080
echo 👤 Демо-вход: demo@autoinvest.by / demo12345
echo ⚙️  Для остановки сервера нажмите: Ctrl + C
echo ========================================================
echo.

rem Открываем браузер через 2 секунды после старта
start "" cmd /c "timeout /t 2 /nobreak > nul & start http://localhost:8080"

rem Запуск сервера
bin\autoinvest.exe

pause
