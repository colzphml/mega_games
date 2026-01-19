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
- `HEALTH_ADDR`
- `TZ`

## Healthcheck

`GET /health` возвращает `200`, когда Discord‑клиент готов.

Проверка:
```
docker compose exec discord-kafka-listener wget -qO- http://127.0.0.1:8080/health
```

## Полезные команды

Посмотреть, что приходит в Kafka:
```
docker compose exec kafka kafka-console-consumer --bootstrap-server kafka:9092 --topic ${KAFKA_INPUT_TOPIC} --from-beginning
```

## Траблшутинг

- **`discord not ready` в health** — проверь токен и права бота на канал.
- **Нет сообщений в Kafka** — убедись, что бот видит канал и что `KAFKA_BROKERS` доступен.
