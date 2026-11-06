#!/usr/bin/env sh
# Сборка AutoInvest на macOS и Linux: фронтенд (если есть Node.js) и Go-бинарник bin/autoinvest.
set -eu
cd "$(dirname "$0")/.."

if command -v npm >/dev/null 2>&1; then
  echo "[1/2] Фронтенд → web/dist"
  (cd web/app && { [ -d node_modules ] || npm ci --no-audit --no-fund; } && npm run build)
else
  echo "[1/2] Node.js не найден — используется готовая сборка web/dist"
fi

if [ ! -f web/dist/index.html ]; then
  echo "Нет web/dist/index.html: установите Node.js 20+ и повторите" >&2
  exit 1
fi

echo "[2/2] Go → bin/autoinvest"
mkdir -p bin
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/autoinvest ./cmd/autoinvest
echo "Готово: bin/autoinvest"
