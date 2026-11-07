#!/usr/bin/env sh
# Резервная копия базы AutoInvest. Запускать по расписанию (cron/systemd timer) раз в час:
#   0 * * * * /opt/autoinvest/scripts/backup.sh >> /var/log/autoinvest-backup.log 2>&1
#
# Переменные:
#   BACKUP_DIR         куда класть копии (по умолчанию ./backups)
#   AUTOINVEST_BIN     как вызвать программу (без Docker), по умолчанию ./bin/autoinvest
#   BACKUP_DOCKER=1    сервис запущен через deploy/docker-compose.yml: копия делается внутри контейнера
#                      и забирается на сервер в BACKUP_DIR (например /opt/autoinvest/backups)
#   RESTIC_REPOSITORY  если задан — копия ещё и отправляется в restic (шифрование, хранение в другом ЦОД)
#   RESTIC_PASSWORD    пароль репозитория restic
set -eu
cd "$(dirname "$0")/.."

BACKUP_DIR="${BACKUP_DIR:-backups}"
AUTOINVEST_BIN="${AUTOINVEST_BIN:-./bin/autoinvest}"
STAMP="$(date +%Y%m%d-%H%M%S)"
FILE="$BACKUP_DIR/hourly/autoinvest-$STAMP.db"

mkdir -p "$BACKUP_DIR/hourly" "$BACKUP_DIR/daily"
if [ "${BACKUP_DOCKER:-}" = "1" ]; then
  # внутри контейнера своя файловая система: делаем копию там и забираем её на сервер
  COMPOSE="docker compose -f deploy/docker-compose.yml"
  TMP=/var/lib/autoinvest/backup-tmp.db
  $COMPOSE exec -T app autoinvest backup "$TMP"
  $COMPOSE cp "app:$TMP" "$FILE"
  $COMPOSE exec -T app rm -f "$TMP"
  if $COMPOSE exec -T app test -d /var/lib/autoinvest/kyc; then
    $COMPOSE exec -T app tar -czf - -C /var/lib/autoinvest kyc > "$BACKUP_DIR/hourly/kyc-$STAMP.tar.gz"
  fi
else
  $AUTOINVEST_BIN backup "$FILE"
  # каталог kyc с файлами документов — рядом с базой
  if [ -d "${DATA_DIR:-data}/kyc" ]; then
    tar -czf "$BACKUP_DIR/hourly/kyc-$STAMP.tar.gz" -C "${DATA_DIR:-data}" kyc
  fi
fi

# одна копия в сутки переносится в daily
TODAY="$(date +%Y%m%d)"
if ! ls "$BACKUP_DIR/daily/" 2>/dev/null | grep -q "$TODAY"; then
  cp "$FILE" "$BACKUP_DIR/daily/autoinvest-$TODAY.db"
fi

# ротация: 24 часовых и 30 дневных копий
ls -1t "$BACKUP_DIR"/hourly/autoinvest-*.db 2>/dev/null | tail -n +25 | xargs -r rm -f
ls -1t "$BACKUP_DIR"/hourly/kyc-*.tar.gz 2>/dev/null | tail -n +25 | xargs -r rm -f
ls -1t "$BACKUP_DIR"/daily/autoinvest-*.db 2>/dev/null | tail -n +31 | xargs -r rm -f

if [ -n "${RESTIC_REPOSITORY:-}" ]; then
  restic backup --tag autoinvest "$FILE" $( [ -f "$BACKUP_DIR/hourly/kyc-$STAMP.tar.gz" ] && echo "$BACKUP_DIR/hourly/kyc-$STAMP.tar.gz" )
  restic forget --tag autoinvest --keep-hourly 48 --keep-daily 30 --keep-monthly 12 --prune
fi
echo "$(date -Iseconds) backup ok: $FILE"
