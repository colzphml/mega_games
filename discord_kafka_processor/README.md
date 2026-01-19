# Discord Kafka Processor

Сервис берет ID сообщений из Kafka, достает сообщение из Discord, парсит неделю/игры и пишет результаты в Kafka. Также ведет статусы обработки в Postgres.

## Поток данных

- Вход: `KAFKA_INPUT_TOPIC` (ID сообщений).
- Выход:
  - `KAFKA_WEEK_TOPIC` — JSON `{ "season": "regular|preseason|postseason", "week": <int> }`
  - `KAFKA_GAME_TOPIC` — строка `game_id`

## Переменные окружения

Обязательные:
- `DISCORD_TOKEN`
- `DISCORD_CHANNEL_ID`
- `KAFKA_BROKERS`
- `KAFKA_INPUT_TOPIC`
- `KAFKA_WEEK_TOPIC`
- `KAFKA_GAME_TOPIC`
- `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`

Опциональные:
- `KAFKA_CONSUMER_GROUP`, `KAFKA_CLIENT_ID`
- `KAFKA_WRITE_TIMEOUT`, `KAFKA_READ_TIMEOUT`
- `DISCORD_HEALTH_INTERVAL`, `DISCORD_EMBED_WARMUP`
- `DISCORD_FETCH_MAX_ATTEMPTS`, `DISCORD_FETCH_RETRY_DELAY`, `DISCORD_FETCH_TIMEOUT`
- `PROCESS_MAX_ATTEMPTS`, `PROCESS_RETRY_INTERVAL`
- `HEALTH_ADDR`, `TZ`, `POSTGRES_*`

## Статусы в Postgres

Таблицы:
- `discord_message_status`
- `discord_message_failed`

Статусы: `new`, `processed`, `failed` (перенос после лимита попыток).

## Healthcheck

`/health` проверяет Kafka + Postgres + доступность Discord API.

```
docker compose exec discord-kafka-processor wget -qO- http://127.0.0.1:8080/health
```

## Траблшутинг

- **Пустые embeds** — увеличь `DISCORD_EMBED_WARMUP` и `DISCORD_FETCH_MAX_ATTEMPTS`.
- **`Unknown Topic Or Partition`** — проверь, что `kafka-init` создал топики.
