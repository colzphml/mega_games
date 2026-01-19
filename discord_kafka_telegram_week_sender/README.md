# Telegram Week Sender

Сервис читает готовый текст недели из Kafka и отправляет его в Telegram. Статусы обработки ведет в Postgres.

## Поток данных

- Вход: `KAFKA_TELEGRAM_WEEK_TOPIC`
- Выход: Telegram‑чат `TELEGRAM_WEEK_CHAT_ID`

## Переменные окружения

Обязательные:
- `KAFKA_BROKERS`
- `KAFKA_TELEGRAM_WEEK_TOPIC`
- `TELEGRAM_BOT_TOKEN`
- `TELEGRAM_WEEK_CHAT_ID`
- `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`

Опциональные:
- `KAFKA_TELEGRAM_WEEK_SENDER_GROUP`, `KAFKA_TELEGRAM_WEEK_SENDER_CLIENT_ID`
- `KAFKA_READ_TIMEOUT`
- `PROCESS_MAX_ATTEMPTS`, `PROCESS_RETRY_INTERVAL`
- `HEALTH_ADDR`, `TZ`, `POSTGRES_*`

## Статусы в Postgres

Таблицы:
- `telegram_week_status`
- `telegram_week_failed`

## Healthcheck

`/health` проверяет Kafka + Postgres.

```
docker compose exec discord-kafka-telegram-week-sender wget -qO- http://127.0.0.1:8080/health
```

## Траблшутинг

- **`401 Unauthorized`** — неверный `TELEGRAM_BOT_TOKEN`.
- **Сообщения не приходят** — проверь чат ID и права бота в чате.
