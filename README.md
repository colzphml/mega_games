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
- `admin-panel` — веб-интерфейс для мониторинга статусов сообщений и управления командами.
- `discord_tools` — утилиты (дамп Discord‑сообщений и генерация SQL из CSV).
- `autoheal` — перезапускает контейнеры со статусом `unhealthy`.
- `loki`, `promtail`, `grafana` — стек мониторинга и логов.

## Развертывание (Workflow v4.1.0+)

Проект использует **Local Docker Registry** (`192.168.0.61:5000`) для ускорения деплоя на Raspberry Pi (ARM). Сборка выполняется на мощной Dev-машине, а Pi просто скачивает готовые образы.

### 1. Сборка и публикация (на Dev-машине)

1. Установите переменную `TAG` в `.env` (например, `4.1.0`).
2. Запустите скрипт:
   ```bash
   ./scripts/publish.sh
   ```
   Скрипт соберет Docker-образы для всех сервисов и отправит их в реестр `192.168.0.61:5000`.

### 2. Деплой (на Raspberry Pi)

1. Зайдите на сервер.
2. Обновите код и запустите скрипт деплоя:
   ```bash
   git pull
   ./scripts/deploy.sh
   ```
   Скрипт попытается скачать образы из реестра. Если реестр недоступен или образы отсутствуют, он автоматически перейдет к локальной сборке (fallback).

## Установка с нуля (curl)

Скрипт установки скачивает релиз, настраивает `.env` и запускает проект.

```bash
curl -fsSL https://raw.githubusercontent.com/colzphml/mega_games/v4.1.0/scripts/install.sh \
  | TAG=v4.1.0 INSTALL_DIR=/opt/mega_games bash
```

Параметры:
- `TAG`: версия релиза (git tag).
- `INSTALL_DIR`: куда установить проект.
- `NONINTERACTIVE=1`: пропустить вопросы (если `.env` уже создан или устраивают дефолты).

## Admin Panel

Доступна на порту `8081`.
- **Unified Status**: Единая таблица всех сообщений. Отображает путь сообщения через все сервисы, ошибки, Game ID и ссылки на изображения.
- **Teams**: Управление списком команд (добавление, редактирование).
- **Logs**: Встроенный дашборд Grafana (Loki).

*Примечание:* Если вы используете SSH-туннель (порт 8081), для работы логов нужно пробросить и порт 3000 (Grafana):
```bash
ssh -L 8081:localhost:8081 -L 3000:localhost:3000 pi@192.168.0.61
```

## Быстрый старт (локально)

1) Скопировать `.env.example` в `.env` и заполнить токены Discord/Telegram.
2) Запуск:
   ```bash
   docker compose up -d
   ```
3) (Опционально) Selenium: `COMPOSE_PROFILES=selenium docker compose up -d`

## Полезные команды

Посмотреть логи:
```bash
docker compose logs -f <service>
```

Проверить health:
```bash
docker compose exec <service> wget -qO- http://127.0.0.1:8080/health
```

Kafka Consumer:
```bash
docker compose exec kafka kafka-console-consumer --bootstrap-server kafka:9092 --topic <topic> --from-beginning
```

## Где что хранится

- **Postgres**: статусы обработки всех сервисов.
- **MongoDB**: метаданные изображений.
- **MinIO**: файлы изображений.
- **Loki**: логи контейнеров.

## Траблшутинг

- **AccessDenied на MinIO**: проверьте bucket policy (public read) или используйте временные ссылки.
- **Grafana "refused to connect"**: убедитесь, что порт 3000 проброшен или доступен с вашего IP.
- **MongoDB Illegal instruction (ARM)**: используйте `MONGO_IMAGE` в `.env` для совместимой версии (если требуется).
