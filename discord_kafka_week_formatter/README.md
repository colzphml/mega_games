# Discord Kafka Week Formatter

Сервис берет обновления недели из Kafka, формирует текст для Telegram и пишет его в Kafka. Также ведет статусы обработки в Postgres.

## Поток данных

- Вход: `KAFKA_WEEK_TOPIC` (JSON `{season, week}`)
- Выход: `KAFKA_TELEGRAM_WEEK_TOPIC` (готовый текст)

## Переменные окружения

Обязательные:
- `KAFKA_BROKERS`
- `KAFKA_WEEK_TOPIC`
- `KAFKA_TELEGRAM_WEEK_TOPIC`
- `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`

Опциональные:
- `KAFKA_WEEK_CONSUMER_GROUP`, `KAFKA_WEEK_CLIENT_ID`
- `KAFKA_READ_TIMEOUT`, `KAFKA_WRITE_TIMEOUT`
- `PROCESS_MAX_ATTEMPTS`, `PROCESS_RETRY_INTERVAL`
- `HEALTH_ADDR`, `TZ`, `POSTGRES_*`

## Данные расписания

Таблицы, которые должны быть заполнены:
- `teams`
- `schedule_games`

Если расписания нет, сервис отправит только заголовок недели + стандартный блок с дедлайном.

## Статусы в Postgres

Таблицы:
- `week_message_status`
- `week_message_failed`

## Healthcheck

`/health` проверяет Kafka + Postgres.

```
docker compose exec discord-kafka-week-formatter wget -qO- http://127.0.0.1:8080/health
```

## Траблшутинг

- **Нет расписания** — наполни `teams` и `schedule_games` через `discord_tools`.
- **Не уходит Post Season** — поддерживается `Pre/Regular/Post Season`.

## Операционные команды (v4.3.0+)

Сборка и публикация только этого сервиса:

```bash
TAG=4.3.0 docker compose build discord-kafka-week-formatter
TAG=4.3.0 docker compose push discord-kafka-week-formatter
```

Обновление на целевом хосте:

```bash
TAG=4.3.0 docker compose pull discord-kafka-week-formatter
TAG=4.3.0 docker compose up -d --no-deps --force-recreate discord-kafka-week-formatter
```

Общий release/deploy workflow и SSH-туннели см. в корневом `README.md`.
