#!/usr/bin/env sh
# Запуск AutoInvest на macOS и Linux: пересобирает Go-бинарник (быстро, с кэшем) и стартует сервер.
#   ./scripts/run.sh          — сервер на http://localhost:8080
#   ./scripts/run.sh demo     — создать демо-аккаунт demo@autoinvest.by / demo12345
#   ./scripts/run.sh admin you@mail.by пароль123
set -eu
cd "$(dirname "$0")/.."

if command -v go >/dev/null 2>&1; then
  mkdir -p bin
  CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/autoinvest ./cmd/autoinvest
elif [ ! -x bin/autoinvest ]; then
  echo "Go не найден и нет собранного bin/autoinvest. Установите Go 1.24+: https://go.dev/dl/" >&2
  exit 1
fi

if [ "$#" -gt 0 ]; then
  exec ./bin/autoinvest "$@"
fi

PORT_URL="${PUBLIC_URL:-http://localhost:8080}"
echo "AutoInvest: $PORT_URL  (остановка — Ctrl+C)"
# открываем браузер, когда сервер ответит
(
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    sleep 1
    if curl -fs "$PORT_URL/healthz" >/dev/null 2>&1; then
      if command -v open >/dev/null 2>&1; then open "$PORT_URL"; elif command -v xdg-open >/dev/null 2>&1; then xdg-open "$PORT_URL" >/dev/null 2>&1; fi
      break
    fi
  done
) &
exec ./bin/autoinvest
