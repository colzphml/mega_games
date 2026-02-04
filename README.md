# MEGA Games Bot

Go-микросервисы для цепочки Discord -> Kafka -> обработка -> Telegram.

## Сервисы

- `discord_kafka_listener` — читает Discord-канал, пишет ID сообщений в Kafka.
- `discord_kafka_processor` — разбирает сообщения на week/game события.
- `discord_kafka_week_formatter` — формирует текст недели для Telegram.
- `discord_kafka_game_image` — генерирует/забирает recap-картинки, кладет в MinIO.
- `discord_kafka_telegram_week_sender` — отправляет week-текст в Telegram.
- `discord_kafka_telegram_game_sender` — отправляет game-картинки в Telegram.
- `admin-panel` — статусы и админ-интерфейс.
- `loki`, `promtail`, `grafana` — логирование и мониторинг.
- `autoheal` — перезапуск unhealthy контейнеров.

## Data Flow

1. `discord_kafka_listener` -> `KAFKA_INPUT_TOPIC`
2. `discord_kafka_processor` -> `KAFKA_WEEK_TOPIC` и `KAFKA_GAME_TOPIC`
3. `discord_kafka_week_formatter` -> `KAFKA_TELEGRAM_WEEK_TOPIC`
4. `discord_kafka_game_image` -> `KAFKA_GAME_IMAGE_TOPIC` (+ MinIO/Mongo/Postgres)
5. Telegram sender-сервисы отправляют в Telegram-чаты

## Release Workflow (v4.2.0+)

### 1) Build + Push образов (Dev-машина)

```bash
cd /Users/colz/gitrepos/envs/mega_games
TAG=4.2.0 docker compose build
TAG=4.2.0 docker compose push
```

Быстрый вариант скриптом:

```bash
cd /Users/colz/gitrepos/envs/mega_games
TAG=4.2.0 ./scripts/publish.sh
```

### 2) Обновление на Raspberry Pi

```bash
ssh pi '
  set -euo pipefail
  cd /home/colz/envs/mega_games
  git checkout release-4.0
  git pull --ff-only origin release-4.0
  export TAG=4.2.0
  docker compose pull
  docker compose up -d --force-recreate
  docker compose ps
'
```

Быстрый вариант скриптом:

```bash
ssh pi 'cd /home/colz/envs/mega_games && TAG=4.2.0 ./scripts/deploy.sh'
```

### 3) Если нет доступа к Raspberry / вашему registry

Запуск на любом Linux/macOS хосте с Docker (локальная сборка без registry):

```bash
git clone https://github.com/colzphml/mega_games.git
cd mega_games
cp .env.example .env
# заполните токены и ключи в .env
docker compose up -d --build
```

Обновление на таком хосте:

```bash
git pull --ff-only
docker compose up -d --build --remove-orphans
```

## SSH Tunnel и URL-ы

Если сервисы подняты на Raspberry и нужны локально:

```bash
ssh -N \
  -L 8081:127.0.0.1:8081 \
  -L 9000:127.0.0.1:9000 \
  -L 3000:127.0.0.1:3000 \
  pi
```

После туннеля:

- Admin Panel: `http://localhost:8081`
- Картинки MinIO: `http://localhost:9000/game-images/...`
- Grafana: `http://localhost:3000`

Если вы в одной сети с Raspberry и порты открыты:

- Admin Panel: `http://<RASPBERRY_IP>:8081`
- Картинки MinIO: `http://<RASPBERRY_IP>:9000/game-images/...`
- Grafana: `http://<RASPBERRY_IP>:3000`

## MinIO: доступ к картинкам

Для открытия прямых ссылок на объекты нужен публичный download policy для bucket `game-images`:

```bash
docker compose exec -T minio sh -lc '
  mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" &&
  mc anonymous set download local/game-images &&
  mc anonymous get local/game-images
'
```

## Установка через curl

```bash
curl -fsSL https://raw.githubusercontent.com/colzphml/mega_games/v4.2.0/scripts/install.sh \
  | TAG=v4.2.0 INSTALL_DIR=/opt/mega_games bash
```

Параметры:

- `TAG` — git tag релиза.
- `INSTALL_DIR` — путь установки.
- `NONINTERACTIVE=1` — без интерактива.

## Полезные команды

Логи:

```bash
docker compose logs -f <service>
```

Healthcheck:

```bash
docker compose exec <service> wget -qO- http://127.0.0.1:8080/health
```

Kafka consumer:

```bash
docker compose exec kafka kafka-console-consumer \
  --bootstrap-server kafka:9092 \
  --topic <topic> \
  --from-beginning
```

## Хранилища

- Postgres — статусы обработки.
- MongoDB — метаданные game image.
- MinIO — файлы картинок.
- Loki — логи контейнеров.
