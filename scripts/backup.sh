#!/usr/bin/env sh
# Резервная копия базы AutoInvest. Запускать по расписанию (cron/systemd timer) раз в час:
#   0 * * * * /opt/autoinvest/scripts/backup.sh >> /var/log/autoinvest-backup.log 2>&1
#
# Переменные:
#   BACKUP_DIR         куда класть копии (по умолчанию ./backups)
#   AUTOINVEST_BIN     как вызвать программу; для Docker:
#                      AUTOINVEST_BIN="docker compose -f deploy/docker-compose.yml exec -T app autoinvest"
#                      (тогда BACKUP_DIR должен быть внутри тома данных, например /var/lib/autoinvest/backups)
#   RESTIC_REPOSITORY  если задан — копия ещё и отправляется в restic (шифрование, хранение в другом ЦОД)
#   RESTIC_PASSWORD    пароль репозитория restic
set -eu
cd "$(dirname "$0")/.."

BACKUP_DIR="${BACKUP_DIR:-backups}"
AUTOINVEST_BIN="${AUTOINVEST_BIN:-./bin/autoinvest}"
STAMP="$(date +%Y%m%d-%H%M%S)"
FILE="$BACKUP_DIR/hourly/autoinvest-$STAMP.db"

mkdir -p "$BACKUP_DIR/hourly" "$BACKUP_DIR/daily"
$AUTOINVEST_BIN backup "$FILE"

# каталог kyc с файлами документов — рядом с базой
if [ -d "${DATA_DIR:-data}/kyc" ]; then
  tar -czf "$BACKUP_DIR/hourly/kyc-$STAMP.tar.gz" -C "${DATA_DIR:-data}" kyc
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
