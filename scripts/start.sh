#!/usr/bin/env sh
# AutoInvest — настоящая версия на этом компьютере (до запуска): лист ожидания + демо-счёт.
# Своя база data-real/, ключи бота, Google и почты — из .env. Деньги не принимаются: человек настраивает копилку
# и встаёт в лист ожидания, а попробовать всё может на демо-счёте с виртуальными деньгами.
#   ./scripts/start.sh               — запустить на http://localhost:8090
#   ./scripts/start.sh stop          — остановить
set -eu
cd "$(dirname "$0")/.."

export DATA_DIR=data-real PAYMENTS=sandbox BROKER=paper MARKET=live APP_ENV=development LAUNCH_MODE=waitlist
export KYC_DOCS=manual MSI_MODE=off OAUTH_APPLE=off
export ADDR="${ADDR:-:8090}" PUBLIC_URL="${PUBLIC_URL:-http://localhost:8090}"

PORT="${ADDR##*:}"
# pid процесса, который слушает порт стенда (пусто, если порт свободен)
stand_pid() { lsof -nP -t -iTCP:"$PORT" -sTCP:LISTEN 2>/dev/null | head -1; }

if [ "${1:-}" = "stop" ]; then
  pid="$(stand_pid)"
  if [ -n "$pid" ]; then
    if ps -p "$pid" -o comm= | grep -q autoinvest; then
      kill "$pid" && echo "AutoInvest на порту $PORT остановлен"
      i=0; while [ -n "$(stand_pid)" ] && [ $i -lt 50 ]; do sleep 0.1; i=$((i+1)); done
    else
      echo "Порт $PORT занят другой программой ($(ps -p "$pid" -o comm=)), её не трогаю."; exit 1
    fi
  elif [ "${1:-}" = "stop" ]; then
    echo "AutoInvest не запущен"
  fi
  [ "${1:-}" = "stop" ] && exit 0
fi

pid="$(stand_pid)"
if [ -n "$pid" ]; then
  if ps -p "$pid" -o comm= | grep -q autoinvest; then
    echo "AutoInvest уже запущен: $PUBLIC_URL — просто откройте его в браузере."
    echo "Перезапустить: make stop && make start"
    exit 0
  fi
  echo "Порт $PORT занят другой программой ($(ps -p "$pid" -o comm=)). Запустите на другом порту:"
  echo "  ADDR=:8091 PUBLIC_URL=http://localhost:8091 ./scripts/start.sh"
  exit 1
fi


if command -v go >/dev/null 2>&1; then
  mkdir -p bin
  CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/autoinvest ./cmd/autoinvest
fi

cat <<'TXT'
================================================================
 AutoInvest (до запуска): http://localhost:8090
 Регистрация → анкета → копилка → лист ожидания + демо-счёт.
 Админ: ADMIN_EMAIL и ADMIN_PASSWORD в .env (создаётся при старте)
        или: DATA_DIR=data-real ./bin/autoinvest admin почта пароль
 Тестовый стенд с фейковыми картами: make test-stand (порт 8092)
================================================================
TXT
exec ./bin/autoinvest
