#!/usr/bin/env sh
# Тестовый стенд для ручной проверки: отдельная база data-test/, тестовая платёжка, учебные покупки
# по реальным ценам. Настоящие деньги здесь не участвуют ни при каких настройках .env.
#   ./scripts/test-stand.sh          — запустить (создаст демо-аккаунт и тестового админа)
#   ./scripts/test-stand.sh reset    — стереть тестовую базу и начать с чистого листа
#   ./scripts/test-stand.sh stop     — остановить стенд, запущенный в фоне или в другом окне
set -eu
cd "$(dirname "$0")/.."

export DATA_DIR=data-test PAYMENTS=sandbox BROKER=paper MARKET=live APP_ENV=development LAUNCH_MODE=open
export ADDR="${ADDR:-:8092}" PUBLIC_URL="${PUBLIC_URL:-http://localhost:8092}"
export BEPAID_SHOP_ID= BEPAID_SECRET_KEY= BEPAID_TEST=true
# стенд не трогает настоящие ключи из .env: тестовый Telegram и тестовая страница входа через Google
export TELEGRAM_BOT_TOKEN= TELEGRAM_SUPPORT_CHAT_ID= GOOGLE_CLIENT_ID= GOOGLE_CLIENT_SECRET= OAUTH_GOOGLE=sandbox SMTP_HOST=

PORT="${ADDR##*:}"
# pid процесса, который слушает порт стенда (пусто, если порт свободен)
stand_pid() { lsof -nP -t -iTCP:"$PORT" -sTCP:LISTEN 2>/dev/null | head -1; }

if [ "${1:-}" = "stop" ] || [ "${1:-}" = "reset" ]; then
  pid="$(stand_pid)"
  if [ -n "$pid" ]; then
    if ps -p "$pid" -o comm= | grep -q autoinvest; then
      kill "$pid" && echo "Стенд на порту $PORT остановлен"
      i=0; while [ -n "$(stand_pid)" ] && [ $i -lt 50 ]; do sleep 0.1; i=$((i+1)); done
    else
      echo "Порт $PORT занят другой программой ($(ps -p "$pid" -o comm=)), её не трогаю."; exit 1
    fi
  elif [ "${1:-}" = "stop" ]; then
    echo "Стенд не запущен"
  fi
  [ "${1:-}" = "stop" ] && exit 0
fi

pid="$(stand_pid)"
if [ -n "$pid" ]; then
  if ps -p "$pid" -o comm= | grep -q autoinvest; then
    echo "Стенд уже запущен: $PUBLIC_URL — просто откройте его в браузере."
    echo "Перезапустить: make test-stand-stop && make test-stand"
    exit 0
  fi
  echo "Порт $PORT занят другой программой ($(ps -p "$pid" -o comm=)). Запустите на другом порту:"
  echo "  ADDR=:8091 PUBLIC_URL=http://localhost:8091 ./scripts/test-stand.sh"
  exit 1
fi

if [ "${1:-}" = "reset" ]; then
  rm -rf data-test
  echo "Тестовая база удалена"
fi

if command -v go >/dev/null 2>&1; then
  mkdir -p bin
  CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/autoinvest ./cmd/autoinvest
fi

./bin/autoinvest demo
./bin/autoinvest admin admin@autoinvest.test admin12345

cat <<'TXT'
================================================================
 Тестовый стенд: http://localhost:8092
 Пользователь:   demo@autoinvest.by / demo12345
 Админка:        admin@autoinvest.test / admin12345  (/admin)
 Тестовые карты: 4200 0000 0000 0000 — всё проходит
                 4005 5500 0000 0019 — привязка есть, списания отклоняются
 Инструкция:     ТЕСТИРОВАНИЕ_ВРУЧНУЮ.md
================================================================
TXT
exec ./bin/autoinvest
