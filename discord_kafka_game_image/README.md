# Discord Kafka Game Image

Сервис читает сообщения с номерами игр из Kafka, скачивает recap‑картинку, сохраняет файл в MinIO и метаданные/статус обработки в MongoDB.

## MongoDB (Compass)

Подключение с локальной машины (Compass):

```
mongodb://megagames:megagames@localhost:27017/?authSource=admin
```

Если порт переопределен:

```
mongodb://megagames:megagames@localhost:<MONGO_EXPOSE_PORT>/?authSource=admin
```

Изнутри docker‑сети (для сервисов, не для Compass):

```
mongodb://megagames:megagames@mongo:27017/?authSource=admin
```

Коллекции:
- `game_images` — основной статус обработки
- `game_images_failed` — упавшие после лимита попыток

## MinIO: временные ссылки на файлы

Сервис сохраняет обычный URL, но бакет по умолчанию приватный.  
Для временной ссылки используйте MinIO Client (`mc`).

Установка на macOS:

```
brew install minio/stable/mc
```

Добавить alias:

```
mc alias set local http://localhost:9000 megagames megagames
```

Получить временную ссылку на объект:

```
mc share download local/game-images/<object_key> --expire 24h
```

`<object_key>` берется из Mongo поля `image.image_object` или из Kafka‑события.

## MinIO: сделать бакет публичным (опционально)

Если нужны постоянные открытые ссылки:

```
mc anonymous set download local/game-images
```

Это откроет доступ на чтение ко всем объектам в бакете.
