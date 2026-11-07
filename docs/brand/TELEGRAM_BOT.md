# Оформление Telegram-бота @AutoInvestAgent

Токен — только в `.env` (`TELEGRAM_BOT_TOKEN`), в репозиторий не коммитить. Если токен попал в чужие руки — @BotFather → `/revoke`.

## Что ставится автоматически при запуске сервиса
- Описание в пустом чате («Что умеет бот») и короткое «о боте» — `internal/telegram/bot.go` (`botDescription`, `botAbout`).
- Список команд: /menu, /balance, /plans, /history, /support, /phone, /stop.

## Что сделать вручную в @BotFather (Bot API не умеет)
1. Аватарка: `/setuserpic` → выбрать бота → отправить `telegram-avatar.png` из этой папки (640×640, Telegram обрежет в круг).
2. Имя бота уже «AutoInvest». При желании: `/setname`.
3. Группа поддержки: создать группу, добавить бота, узнать её ID (например, переслать сообщение из группы боту @userinfobot)
   и записать в `.env` как `TELEGRAM_SUPPORT_CHAT_ID` — туда будут приходить обращения, ответ reply уходит клиенту.
   Чтобы бот видел ответы в группе: @BotFather → `/setprivacy` → Disable.
