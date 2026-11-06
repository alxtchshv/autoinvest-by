#!/usr/bin/env sh
# Тестовый стенд для ручной проверки: отдельная база data-test/, тестовая платёжка, учебные покупки
# по реальным ценам. Настоящие деньги здесь не участвуют ни при каких настройках .env.
#   ./scripts/test-stand.sh          — запустить (создаст демо-аккаунт и тестового админа)
#   ./scripts/test-stand.sh reset    — стереть тестовую базу и начать с чистого листа
set -eu
cd "$(dirname "$0")/.."

export DATA_DIR=data-test PAYMENTS=sandbox BROKER=paper MARKET=live APP_ENV=development
export ADDR="${ADDR:-:8090}" PUBLIC_URL="${PUBLIC_URL:-http://localhost:8090}"
export BEPAID_SHOP_ID= BEPAID_SECRET_KEY= BEPAID_TEST=true

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
 Тестовый стенд: http://localhost:8090
 Пользователь:   demo@autoinvest.by / demo12345
 Админка:        admin@autoinvest.test / admin12345  (/admin)
 Тестовые карты: 4200 0000 0000 0000 — всё проходит
                 4005 5500 0000 0019 — привязка есть, списания отклоняются
 Инструкция:     ТЕСТИРОВАНИЕ_ВРУЧНУЮ.md
================================================================
TXT
exec ./bin/autoinvest
