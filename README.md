# MEGA Games Bot

Набор сервисов, который забирает новые сообщения из Discord, парсит обновления недели/игр, готовит сообщения и изображения, а затем отправляет все в Telegram. Все сервисы запускаются через `docker-compose.yml` и управляются через `.env`.

## Архитектура потока

1) `discord_kafka_listener` читает канал Discord и пишет ID сообщений в Kafka (`KAFKA_INPUT_TOPIC`).
2) `discord_kafka_processor` берет ID, достает сообщение из Discord, парсит:
   - недели → `KAFKA_WEEK_TOPIC` (JSON `{season, week}`)
   - игры → `KAFKA_GAME_TOPIC` (строка game_id)
3) `discord_kafka_week_formatter` берет недели, формирует текст для Telegram и пишет в `KAFKA_TELEGRAM_WEEK_TOPIC`.
4) `discord_kafka_game_image` берет игры, получает картинку, сохраняет файл в MinIO, метаданные в Mongo+Postgres и пишет событие в `KAFKA_GAME_IMAGE_TOPIC`.
5) `discord_kafka_telegram_week_sender` отправляет текст недели в Telegram.
6) `discord_kafka_telegram_game_sender` скачивает картинку из MinIO и отправляет в Telegram с подписью‑ссылкой на игру.

## Сервисы

- `discord_kafka_listener` — слушает Discord и пишет ID сообщений.
- `discord_kafka_processor` — парсит сообщения, пишет недели/игры в Kafka, трекает статус в Postgres.
- `discord_kafka_week_formatter` — формирует текст недели для Telegram, трекает статус в Postgres.
- `discord_kafka_game_image` — получает картинку игры, сохраняет в MinIO, трекает статус в Mongo и Postgres.
- `discord_kafka_telegram_week_sender` — отправляет недельные сообщения в Telegram, трекает статус в Postgres.
- `discord_kafka_telegram_game_sender` — отправляет игровые картинки в Telegram, трекает статус в Postgres.
- `discord_tools` — утилиты (дамп Discord‑сообщений и генерация SQL из CSV).
- `autoheal` — перезапускает контейнеры со статусом `unhealthy`.

## Быстрый старт

1) Скопировать `.env.example` в `.env` и заполнить:
   - `DISCORD_TOKEN`, `DISCORD_CHANNEL_ID`
   - `TELEGRAM_BOT_TOKEN`, `TELEGRAM_WEEK_CHAT_ID`, `TELEGRAM_GAME_CHAT_ID`
2) Запуск:

```
docker compose up -d
```

3) Если нужен Selenium, включи профиль:

```
COMPOSE_PROFILES=selenium docker compose up -d
```

## Установка через curl

Скрипт установки умеет:
- проверить зависимости (docker, compose, curl);
- скачать нужный релиз;
- создать `.env` из `.env.example`;
- при желании задать базовые параметры интерактивно;
- запустить `docker compose up -d --build`.

Пример установки:

```
curl -fsSL https://raw.githubusercontent.com/colzphml/mega_games/v3.0.4/scripts/install.sh \
  | TAG=v3.0.4 INSTALL_DIR=/opt/mega_games bash
```

Если интерактивный режим не нужен:

```
curl -fsSL https://raw.githubusercontent.com/colzphml/mega_games/v3.0.4/scripts/install.sh \
  | TAG=v3.0.4 INSTALL_DIR=/opt/mega_games NONINTERACTIVE=1 bash
```

Примечания:
- поддерживаются Linux/macOS на `amd64` и `arm64` (Raspberry Pi — только 64‑битные ОС);
- интерактив можно пропустить через `NONINTERACTIVE=1` (или предварительно создав `.env` в `INSTALL_DIR`).
- скрипт автоматически выставляет `TARGET_PLATFORM` и `APP_VERSION`.

## Релизы

Релиз — это git‑тег `vX.Y.Z` и (опционально) Release на GitHub.

Минимальные шаги:

```
git tag -a v3.0.4 -m "Release 3.0.4"
git push origin v3.0.4
```

Если используешь GitHub CLI:

```
gh release create v3.0.4 --title "3.0.4" --notes "Mega Games bot release 3.0.4"
```

## Полезные команды

Посмотреть логи сервиса:
```
docker compose logs -f <service>
```

Проверить health:
```
docker compose exec <service> wget -qO- http://127.0.0.1:8080/health
```

Проверить Kafka:
```
docker compose exec kafka kafka-console-consumer --bootstrap-server kafka:9092 --topic <topic> --from-beginning
```

## Траблшутинг

- **Kafka init бесконечно пишет `waiting for kafka`** — проверь `KAFKA_BROKERS`, доступность брокера и что `kafka` поднят.
- **AccessDenied на ссылках MinIO** — бакет приватный. Сделай public read или получай временные ссылки через `mc` (см. `discord_kafka_game_image/README.md`).
- **Изображение "как из headless" при gochrome** — проверь `GAME_IMAGE_FETCHER_TYPE` внутри контейнера и пересоздай сервис.
- **Selenium не запускается** — включи профиль `COMPOSE_PROFILES=selenium`.
- **No space left on device** — очисти Docker cache (`docker system df -v`, `docker builder prune`).

## Где что хранится

- Postgres: статусы обработки (processor, week formatter, game image, telegram senders).
- MongoDB: статусы и метаданные изображений (game image).
- MinIO: файлы изображений игр.

## Данные расписания

Для `discord_kafka_week_formatter` нужны таблицы `teams` и `schedule_games`. Их можно создать и наполнить через `discord_tools` (см. `discord_tools/README.md`).
