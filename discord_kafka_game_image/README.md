# Discord Kafka Game Image

Сервис читает `game_id` из Kafka, получает картинку игры, сохраняет файл в MinIO, метаданные и статус — в MongoDB и Postgres. После успешной обработки публикует событие в Kafka.

## Поток данных

- Вход: `KAFKA_GAME_TOPIC` (строка `game_id` или URL игры)
- Выход: `KAFKA_GAME_IMAGE_TOPIC` (JSON c метаданными изображения)

Пример события:
```json
{
  "message_id": "...",
  "source_message_id": "...",
  "game_id": "5950836",
  "game_url": "https://neonsportz.com/leagues/MEGA/games/5950836",
  "image_url": "http://localhost:9000/game-images/game-recaps/5950836/....png",
  "bucket": "game-images",
  "object_key": "game-recaps/5950836/....png",
  "content_type": "image/png",
  "size": 1564101,
  "stored_at": "2026-01-18T23:32:40+03:00",
  "fetcher": "gochrome"
}
```

## Режимы получения изображения

`GAME_IMAGE_FETCHER_TYPE`:
- `headless` — генерирует картинку по API (без браузера)
- `gochrome` — делает скрин Recap‑страницы через chromedp
- `selenium` — скачивает картинку через Selenium

Если выбран `selenium`, включи профиль:
```
COMPOSE_PROFILES=selenium docker compose up -d
```

## Статусы обработки

MongoDB (основной трекинг + метаданные):
- `game_images`
- `game_images_failed`

Postgres (дублирующий трекинг статуса):
- `game_image_status`
- `game_image_failed`

## Переменные окружения

Обязательные:
- `KAFKA_BROKERS`, `KAFKA_GAME_TOPIC`, `KAFKA_GAME_IMAGE_TOPIC`
- `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`
- `MONGO_URI`, `MONGO_DB`
- `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`, `MINIO_BUCKET`

Опциональные:
- `GAME_IMAGE_FETCHER_TYPE`, `GOCHROME_HEADLESS`, `SELENIUM_URL`
- `MINIO_PUBLIC_URL`, `MINIO_OBJECT_PREFIX`
- `PROCESS_MAX_ATTEMPTS`, `PROCESS_RETRY_INTERVAL`
- `KAFKA_READ_TIMEOUT`, `KAFKA_WRITE_TIMEOUT`, `KAFKA_GAME_IMAGE_CONSUMER_GROUP`
- `HEALTH_ADDR`, `TZ`

## MongoDB (Compass)

```
mongodb://megagames:megagames@localhost:27017/?authSource=admin
```

Если порт переопределен:
```
mongodb://megagames:megagames@localhost:<MONGO_EXPOSE_PORT>/?authSource=admin
```

## MinIO: временные ссылки

Установить MinIO Client:
```
brew install minio/stable/mc
```

Alias:
```
mc alias set local http://localhost:9000 megagames megagames
```

Временная ссылка:
```
mc share download local/game-images/<object_key> --expire 24h
```

Публичный доступ (опционально):
```
mc anonymous set download local/game-images
```

## Healthcheck

`/health` проверяет Mongo + Postgres + MinIO + Kafka.

```
docker compose exec discord-kafka-game-image wget -qO- http://127.0.0.1:8080/health
```

## Траблшутинг

- **AccessDenied в MinIO** — сделай бакет публичным или используй `mc share download`.
- **Картинки дублируются** — проверь `PROCESS_RETRY_INTERVAL` и статус в Postgres/Mongo.
- **gochrome выглядит как headless** — проверь реальный `GAME_IMAGE_FETCHER_TYPE` внутри контейнера.

## Операционные команды (v4.2.0+)

Сборка и публикация только этого сервиса:

```bash
TAG=4.2.0 docker compose build discord-kafka-game-image
TAG=4.2.0 docker compose push discord-kafka-game-image
```

Обновление на целевом хосте:

```bash
TAG=4.2.0 docker compose pull discord-kafka-game-image
TAG=4.2.0 docker compose up -d --no-deps --force-recreate discord-kafka-game-image
```

Общий release/deploy workflow, URL-ы и SSH-туннели см. в корневом `README.md`.
