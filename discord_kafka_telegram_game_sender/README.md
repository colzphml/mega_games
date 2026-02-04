# Telegram Game Sender

Сервис читает события с метаданными изображений из Kafka, скачивает картинку из MinIO и отправляет её в Telegram с подписью‑ссылкой на игру. Статусы обработки ведет в Postgres.

## Поток данных

- Вход: `KAFKA_GAME_IMAGE_TOPIC`
- Выход: Telegram‑чат `TELEGRAM_GAME_CHAT_ID`

## Переменные окружения

Обязательные:
- `KAFKA_BROKERS`
- `KAFKA_GAME_IMAGE_TOPIC`
- `TELEGRAM_BOT_TOKEN`
- `TELEGRAM_GAME_CHAT_ID`
- `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`
- `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`

Опциональные:
- `KAFKA_TELEGRAM_GAME_SENDER_GROUP`, `KAFKA_TELEGRAM_GAME_SENDER_CLIENT_ID`
- `KAFKA_READ_TIMEOUT`
- `PROCESS_MAX_ATTEMPTS`, `PROCESS_RETRY_INTERVAL`
- `MINIO_REGION`, `MINIO_USE_SSL`
- `HEALTH_ADDR`, `TZ`, `POSTGRES_*`

## Статусы в Postgres

Таблицы:
- `telegram_game_status`
- `telegram_game_failed`

## Healthcheck

`/health` проверяет Kafka + Postgres + MinIO.

```
docker compose exec discord-kafka-telegram-game-sender wget -qO- http://127.0.0.1:8080/health
```

## Траблшутинг

- **Не скачивается объект из MinIO** — проверь `MINIO_ENDPOINT` и доступность бакета.
- **Сообщения не приходят** — проверь `TELEGRAM_GAME_CHAT_ID` и права бота.

## Операционные команды (v4.2.0+)

Сборка и публикация только этого сервиса:

```bash
TAG=4.2.0 docker compose build discord-kafka-telegram-game-sender
TAG=4.2.0 docker compose push discord-kafka-telegram-game-sender
```

Обновление на целевом хосте:

```bash
TAG=4.2.0 docker compose pull discord-kafka-telegram-game-sender
TAG=4.2.0 docker compose up -d --no-deps --force-recreate discord-kafka-telegram-game-sender
```

Общий release/deploy workflow и SSH-туннели см. в корневом `README.md`.
