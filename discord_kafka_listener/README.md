# Discord Kafka Listener

Сервис слушает канал Discord и пишет ID новых сообщений в Kafka.

## Поток данных

- Вход: Discord канал (`DISCORD_CHANNEL_ID`).
- Выход: Kafka topic `KAFKA_INPUT_TOPIC`.
- Формат: `key = message_id`, `value = message_id`.

## Переменные окружения

Обязательные:
- `DISCORD_TOKEN`
- `DISCORD_CHANNEL_ID`
- `KAFKA_BROKERS`
- `KAFKA_INPUT_TOPIC`

Опциональные:
- `KAFKA_CLIENT_ID`
- `KAFKA_WRITE_TIMEOUT`
- `MESSAGE_BUFFER_SIZE`
- `MESSAGE_ENQUEUE_TIMEOUT`
- `KAFKA_RETRY_BASE_DELAY`
- `KAFKA_RETRY_MAX_DELAY`
- `KAFKA_RETRY_MAX_ATTEMPTS`
- `KAFKA_RETRY_MAX_PENDING`
- `DISCORD_BACKFILL_PAGE_SIZE`
- `DISCORD_BACKFILL_MAX_MESSAGES`
- `DISCORD_BACKFILL_TIMEOUT`
- `DISCORD_BACKFILL_MIN_INTERVAL`
- `LAST_MESSAGE_ID_PATH`
- `HEALTH_ADDR`
- `TZ`

`LAST_MESSAGE_ID_PATH` используется для сохранения последнего успешно отправленного ID,
чтобы после reconnect можно было добрать сообщения через backfill.

## Healthcheck

`GET /health` возвращает `200`, когда Discord‑клиент готов.

Проверка:
```
docker compose exec discord-kafka-listener wget -qO- http://127.0.0.1:8080/health
```

## Полезные команды

Посмотреть, что приходит в Kafka:
```
docker compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh --bootstrap-server kafka:9092 --topic ${KAFKA_INPUT_TOPIC} --from-beginning
```

## Траблшутинг

- **`discord not ready` в health** — проверь токен и права бота на канал.
- **Нет сообщений в Kafka** — убедись, что бот видит канал и что `KAFKA_BROKERS` доступен.

## Операционные команды (v4.3.1+)

Предпочтительный релизный путь:

```bash
TAG=4.3.1 ./scripts/release.sh
```

Локальная сборка и публикация только этого сервиса остаётся аварийным fallback:

```bash
TAG=4.3.1 docker compose build discord-kafka-listener
TAG=4.3.1 docker compose push discord-kafka-listener
```

Обновление на целевом хосте:

```bash
TAG=4.3.1 docker compose pull discord-kafka-listener
TAG=4.3.1 docker compose up -d --no-deps --force-recreate discord-kafka-listener
```

Общий release/deploy workflow и SSH-туннели см. в корневом `README.md`.
