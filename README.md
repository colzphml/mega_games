# MEGA Games Bot

Go-микросервисы для цепочки Discord -> Kafka -> обработка -> Telegram.

## Сервисы

### Приложение

- `discord-kafka-listener` — читает Discord-канал, пишет ID сообщений в Kafka.
- `discord-kafka-processor` — разбирает сообщения на week/game события, пингует Discord API для healthcheck.
- `discord-kafka-week-formatter` — формирует текст недели для Telegram.
- `discord-kafka-game-image` — генерирует/забирает recap-картинки, кладет в MinIO.
- `discord-kafka-telegram-week-sender` — отправляет week-текст в Telegram.
- `discord-kafka-telegram-game-sender` — отправляет game-картинки в Telegram.
- `admin-panel` — статусы и админ-интерфейс (`:8002`). Закрыт HTTP Basic Auth
  через `ADMIN_USER`/`ADMIN_PASSWORD` (см. `.env.example`); пустой `ADMIN_PASSWORD`
  (значение по умолчанию) отключает проверку — годится только для локальной
  разработки, для хоста, доступного не только с localhost, пароль обязателен.
- `autoheal` — перезапуск unhealthy контейнеров.

### Инфраструктура

- `redpanda` — брокер сообщений, Kafka API-совместим (порт `9092` только
  внутри `megagames-net`, наружу не публикуется).
- `kafka-init` — служебный контейнер, создаёт топики через `rpk` при старте.
- `postgres` — статусы обработки (порт `5432` только внутри `megagames-net`).
- `minio` — файлы картинок, S3-совместимое хранилище (`:9000`; консоль на
  `9001` наружу не публикуется).

Без профиля `monitoring` наружу (на хост) публикуются только `8002` (admin-panel)
и `9000` (MinIO API). Всё остальное доступно лишь внутри docker-сети
`megagames-net`. С профилем `monitoring` добавляется ещё `3000` (Grafana) —
см. ниже.

### Опциональный профиль `monitoring`

По умолчанию выключен: на слабых хостах (Raspberry Pi) связка съедала ~210 МБ RAM
и заметную долю CPU, а логи и так доступны через `docker compose logs`.

- `loki` — агрегация логов (`:3100`).
- `promtail` — сбор логов контейнеров и отправка в Loki.
- `grafana` — дашборды (`:3000`).

```bash
COMPOSE_PROFILES=monitoring docker compose up -d
```

## Data Flow

```
Discord Channel
      │
      ▼
discord-kafka-listener ──► KAFKA_INPUT_TOPIC (ID сообщений)
      │
      ▼
discord-kafka-processor ──┬──► KAFKA_WEEK_TOPIC  (JSON: season, week)
                          └──► KAFKA_GAME_TOPIC   (game_id)
                                    │
              ┌─────────────────────┘
              │
              ├──► discord-kafka-week-formatter ──► KAFKA_TELEGRAM_WEEK_TOPIC
              │                                             │
              │                                             ▼
              │                              discord-kafka-telegram-week-sender ──► Telegram
              │
              └──► discord-kafka-game-image ──► KAFKA_GAME_IMAGE_TOPIC
                         │  (+ MinIO/Postgres)              │
                                                            ▼
                                         discord-kafka-telegram-game-sender ──► Telegram
```

## Release Workflow (v5.0.0+)

`TAG` обязателен везде ниже: ни в `docker-compose.yml`, ни в `release.sh`,
`deploy.sh`, `publish.sh`, `install.sh` дефолта нет — без переменной команды
падают с понятной ошибкой вместо того, чтобы молча откатить прод на старый
образ (так уже случалось, см. `AGENTS.md` → «Антипаттерны»).

### 1) Публикация образов через GitHub Actions

Workflow `.github/workflows/publish-images.yml` собирает и пушит multi-arch образы в GHCR:

- платформы: `linux/amd64`, `linux/arm64`
- registry: `ghcr.io/<owner>/mega_games/<service>`
- триггеры: `push` в `main`, `master`, `release-*`, git tags `v*`, `workflow_dispatch`

Предпочтительный релизный сценарий:

```bash
TAG=5.0.0 ./scripts/release.sh
```

Ручной эквивалент:

```bash
git tag -a v5.0.0 -m "Release 5.0.0"
git push origin v5.0.0
```

Для релизного тега workflow публикует как минимум теги `v5.0.0`, `5.0.0` и `latest`.

Локальная ручная публикация остаётся только как аварийный single-arch fallback:

```bash
TAG=5.0.0 IMAGE_REGISTRY=ghcr.io IMAGE_NAMESPACE=colzphml/mega_games docker compose build
TAG=5.0.0 IMAGE_REGISTRY=ghcr.io IMAGE_NAMESPACE=colzphml/mega_games docker compose push
```

Быстрый вариант скриптом:

```bash
TAG=5.0.0 IMAGE_REGISTRY=ghcr.io IMAGE_NAMESPACE=colzphml/mega_games ./scripts/publish.sh
```

### 2) Обновление на Raspberry Pi / обычном сервере

```bash
ssh pi '
  set -euo pipefail
  cd /home/colz/envs/mega_games
  git checkout release-5.0
  git pull --ff-only origin release-5.0
  export IMAGE_REGISTRY=ghcr.io
  export IMAGE_NAMESPACE=colzphml/mega_games
  export TAG=5.0.0
  docker compose pull
  docker compose up -d --force-recreate --remove-orphans --no-build
  docker compose ps
'
```

Быстрый вариант скриптом:

```bash
ssh pi "cd /home/colz/envs/mega_games && IMAGE_REGISTRY=ghcr.io IMAGE_NAMESPACE=colzphml/mega_games TAG=5.0.0 ./scripts/deploy.sh"
```

Здесь ничего не пушится напрямую в Raspberry Pi:
- GitHub Actions публикует образы в GHCR.
- Хост только делает `docker compose pull` и `docker compose up -d`.

Если на хосте ещё остался старый registry-конфиг, сначала привести runtime к GHCR:

- в `docker-compose.yml` app-образы должны идти через `${IMAGE_REGISTRY}/${IMAGE_NAMESPACE}/...:${TAG}`
- в `.env` должны быть `IMAGE_REGISTRY=ghcr.io` и `IMAGE_NAMESPACE=colzphml/mega_games`
- для нормального релиза использовать `TAG=X.Y.Z`
- временно можно использовать branch tag `release-ghcr-multiarch-actions`, если релизный тег ещё не опубликован

Проверка после деплоя:

```bash
ssh pi '
  set -euo pipefail
  cd /home/colz/envs/mega_games
  docker compose ps
  docker compose exec -T discord-kafka-listener wget -qO- http://127.0.0.1:8080/health
  docker compose exec -T admin-panel wget -qO- http://127.0.0.1:8081/health
'
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

Если сервисы подняты на Raspberry и нужны локально, пробросьте порты:

```bash
ssh -N \
  -L 8002:127.0.0.1:8002 \
  -L 9000:127.0.0.1:9000 \
  -L 3000:127.0.0.1:3000 \
  pi
```

`postgres`, `redpanda` и консоль MinIO (`9001`) наружу не публикуются — им
тоннель не пробросить, доступ только изнутри хоста (`docker compose exec`).
Строка с `3000` (Grafana) имеет смысл, только если на хосте поднят профиль
`monitoring` (`COMPOSE_PROFILES=monitoring`), иначе порт просто не слушает.

После туннеля:

| Сервис | URL |
|--------|-----|
| Admin Panel | `http://localhost:8002` |
| MinIO (прямые ссылки на картинки) | `http://localhost:9000/game-images/...` |
| Grafana (профиль `monitoring`) | `http://localhost:3000` |

Если вы в одной сети с Raspberry и порты открыты:

| Сервис | URL |
|--------|-----|
| Admin Panel | `http://<RASPBERRY_IP>:8002` |
| MinIO (прямые ссылки на картинки) | `http://<RASPBERRY_IP>:9000/game-images/...` |
| Grafana (профиль `monitoring`) | `http://<RASPBERRY_IP>:3000` |

## MinIO: доступ к картинкам

Для открытия прямых ссылок на объекты нужен публичный download policy для bucket `game-images`.
Выполняется один раз после первого запуска:

```bash
docker compose exec -T minio sh -lc '
  mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" &&
  mc anonymous set download local/game-images &&
  mc anonymous get local/game-images
'
```

## Установка через curl

```bash
curl -fsSL https://raw.githubusercontent.com/colzphml/mega_games/v5.0.0/scripts/install.sh \
  | TAG=v5.0.0 INSTALL_DIR=/opt/mega_games bash
```

Параметры:

- `TAG` — git tag релиза.
- `INSTALL_DIR` — путь установки.
- `NONINTERACTIVE=1` — без интерактива.

## Полезные команды

Логи сервиса:

```bash
docker compose logs -f <service>
```

Healthcheck сервиса вручную (порт `8080`; у `admin-panel` — `8081`):

```bash
docker compose exec <service> wget -qO- http://127.0.0.1:8080/health
```

Redpanda consumer (чтение топика):

```bash
docker compose exec redpanda rpk topic consume <topic> --brokers redpanda:9092
```

Статус всех контейнеров:

```bash
docker compose ps
```

## Хранилища

| Хранилище | Назначение | Порт |
|-----------|-----------|------|
| Postgres | Статусы обработки сообщений | `5432`, только внутри `megagames-net` |
| MinIO | Файлы картинок (S3-совместимо) | `9000` (API, публикуется наружу); консоль `9001` только внутри сети |
| Loki | Агрегация логов контейнеров (профиль `monitoring`) | `3100`, только внутри `megagames-net` |
