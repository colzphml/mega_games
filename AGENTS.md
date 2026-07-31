# БАЗА ЗНАНИЙ ПРОЕКТА

## ⚠️ Идёт разработка v5.0

**Новая сессия начинает с этих файлов, по порядку:**

1. `docs/superpowers/specs/2026-07-28-mega-games-v5-design.md` — что делаем и почему
2. `docs/superpowers/plans/2026-07-28-mega-games-v5.md` (+ `-part2`, `-part3`) — разбивка на задачи
3. `docs/superpowers/plans/v5-progress.md` — что из этого уже сделано

`AUDIT.md` (корень репозитория) — исходный разбор системы: откуда взялся каждый
дефект и какие замеры сняты с живого хоста.

**Ветка разработки:** `release-5.0`. В проде сейчас `release-4.0` (`v4.3.9`) — v5.0
в проде ещё нет, релиз — отдельная задача V5-30.

### Правила параллельной работы

Каждая задача v5.0 — отдельный git worktree от `release-5.0`:

```bash
git worktree add ../mega_games-V5-07 -b v5/07-eligibility release-5.0
cd ../mega_games-V5-07
```

- Трогать только файлы из раздела «Files» своей задачи в плане. Поле «Не трогает»
  у задачи (если оно есть) называет, где работают соседние агенты.
- Порядок мержа задан зависимостями задач в плане; где две задачи правят один
  файл, там же написано, какая мержится первой.
- Перед мержем: `go build ./... && go vet ./... && go test ./... -short`.
- После мержа — обновить `docs/superpowers/plans/v5-progress.md`: статус задачи,
  хеш коммита, и запись в журнале решений, если было отклонение от плана.

### Что нельзя делать ни в одной задаче

| Запрещено | Почему |
|---|---|
| Удалять тома `postgres-data`, `minio-data`, `mongo-data`, `kafka-data`, `zookeeper-data`, `zookeeper-log` | Требование заказчика — вся история сохраняется, даже для уже отключённых Mongo, Kafka и Zookeeper |
| Включать lifecycle-политику (автоудаление) в MinIO | Та же причина: ничего не должно стираться само |
| Задавать дефолт для `TAG` в скриптах или compose | Дефолт `4.3.2` уже приводил к молчаливому откату прода на семь версий назад (`AUDIT.md`, P0-1) |
| Собирать релизные образы на Raspberry Pi | Публикация образов — только через GitHub Actions в GHCR |
| Трогать живой хост вне задачи выкатки (V5-30) | Остальные задачи — код и документация, риск для прод-хоста не нужен |

## ОБЗОР

Go-микросервисы: Discord -> Kafka -> Обработка -> Telegram. Событийная архитектура: брокер — Redpanda (Kafka API-совместим, топики и env всё ещё называются `KAFKA_*`), хранение — Postgres (статусы) + MinIO (файлы картинок). MongoDB убрана в v5.0 — была полным дублем `game_image_status` в Postgres.

## СТРУКТУРА

```
mega_games/
├── discord_kafka_listener/       # Discord -> Kafka (ID сообщений)
├── discord_kafka_processor/      # Парсинг недель/игр из Discord
├── discord_kafka_week_formatter/ # Форматирование текста недели для Telegram
├── discord_kafka_game_image/     # Генерация картинок игр (gochrome/headless)
├── discord_kafka_telegram_week_sender/   # Отправка текста недели в Telegram
├── discord_kafka_telegram_game_sender/   # Отправка картинок в Telegram
├── cmd/admin_panel/              # Admin-panel: веб-интерфейс и статусы (:8081 внутри контейнера)
├── internal/common/               # Общее для всех 6 сервисов: config, health, retry, queue, tgmarkdown, ports, harness'ы pgtest/brokertest
├── internal/admin/                # Пакет admin-panel: auth (Basic Auth), store, handlers
├── discord_tools/                 # Утилиты: message-dump, csv-to-sql (свой go.mod, не часть корневого модуля)
├── monitoring/                    # Grafana + Loki + Promtail конфиги
├── scripts/                       # release.sh/deploy.sh/publish.sh/install.sh, host-setup.sh — тюнинг хоста
├── docker-compose.yml             # Оркестрация всего стека
└── go.mod                         # Корневой модуль (Go 1.25.5)
```

Каждый из шести `discord_kafka_*`-сервисов и `admin_panel`: `cmd/<name>/main.go` +
пакеты в собственном `internal/`. Плюс общий `internal/common/` в корне —
используют все шесть kafka-сервисов (не admin-panel, у него свой `internal/admin/`).

## ГДЕ ИСКАТЬ

| Задача | Расположение | Примечания |
|--------|--------------|------------|
| Добавить топик | `docker-compose.yml` (kafka-init) + конфиги сервисов | `rpk topic create` в цикле entrypoint, брокер — Redpanda |
| Изменить парсинг сообщений | `discord_kafka_processor/internal/parser/` | Regex-based |
| Сменить image fetcher | `discord_kafka_game_image/internal/middle/` | `gochrome/`, `headless/` (`selenium/` удалён в v5.0 вместе с профилем compose) |
| Добавить таблицу Postgres | `internal/store/store.go` сервиса (у `game_image` — `internal/pgstore/store.go`) | Автомиграция через `EnsureSchema()` |
| Формат сообщений Telegram | `*_telegram_*_sender/internal/telegram/client.go` | |
| Переменные окружения | `.env.example` | Вся конфигурация через env, ручной парсинг без сторонних библиотек |
| Health сервиса | Каждый `main.go` имеет `/health` | 6 kafka-сервисов — через общий `internal/common/health` (`health.WaitFor` блокирует до готовности зависимости); `admin-panel` — свой хендлер, единственный роут вне Basic Auth |

## ПОТОК ДАННЫХ

```
Discord Channel
      │
      ▼
discord_kafka_listener ──► KAFKA_INPUT_TOPIC (ID сообщений)
      │
      ▼
discord_kafka_processor ──┬──► KAFKA_WEEK_TOPIC (JSON: season, week)
                          └──► KAFKA_GAME_TOPIC (game_id)
      │                              │
      ▼                              ▼
discord_kafka_week_formatter    discord_kafka_game_image
      │                              │
      ▼                              ▼
KAFKA_TELEGRAM_WEEK_TOPIC      KAFKA_GAME_IMAGE_TOPIC
      │                              │
      ▼                              ▼
telegram_week_sender           telegram_game_sender
      │                              │
      └──────────► Telegram ◄────────┘
```

## ХРАНИЛИЩА

| Хранилище | Назначение | Используется |
|-----------|------------|--------------|
| Postgres | Статусы обработки, данные расписания, статусы и метаданные game-image (таблица `game_image_status`, включая `image_url`) | Все сервисы |
| MinIO | Файлы изображений (S3-совместимый) | game_image, telegram_game_sender |
| Redpanda | Событийный обмен между сервисами, Kafka API-совместим (env и топики всё ещё `KAFKA_*`) | Все сервисы |

MongoDB убрана в v5.0 (задача V5-15): `game_image_status` в Postgres уже содержал
всё то же самое (сверено 600/600 записей перед удалением), отдельное хранилище
было полным дублем.

## СОГЛАШЕНИЯ

### Стиль кода
- Логирование: `github.com/rs/zerolog` — структурированный JSON
- Конфигурация: `internal/config/config.go` каждого сервиса — ручной парсинг env
  (`requiredEnv`/`optionalEnv` и аналоги), без сторонних библиотек. В
  `internal/common/config` есть готовые аналоги (`Required`/`Optional`/...),
  но пока ни один сервис на них не переведён
- Нет явного линтера — использовать `go fmt`, `go vet`

### Паттерн сервисов
- 6 kafka-сервисов: healthcheck на `:8080/health`; `admin-panel` — на `:8081/health`
- Graceful shutdown через `signal.NotifyContext`
- Retry с exponential backoff для внешних зависимостей
- `internal/common/health.WaitFor(...)` блокирует запуск до готовности зависимости
  (Postgres, MinIO) — общая замена прежним отдельным `waitForPostgres`/`waitForMongo`/`waitForMinio`

### База данных
- **Идемпотентность обязательна**: все INSERT используют `ON CONFLICT DO NOTHING`
- Схема автомигрируется через `EnsureSchema()` при старте
- Статусы: `new` -> `processed`; у `game_image` и `telegram_game_sender` есть
  промежуточный `in_progress` для долгих операций (рендер картинки, загрузка в Telegram)
- Retry-порог должен быть кратен интервалу тикера, не равен ему (`internal/common/queue.StaleThreshold`,
  используют `telegram_week_sender`, `telegram_game_sender`, `game_image`) — см. антипаттерны

### Docker
- Multi-stage сборка: `golang:1.25.5-alpine` -> `alpine:3.21`
- Версия через `-ldflags` из `git describe --tags --always`
- Non-root пользователь (`app`) в финальном образе
- Почти все сервисы — лейбл `autoheal=true` (нужен healthcheck; нет его у
  `minio` и у одноразового `kafka-init`)

## АНТИПАТТЕРНЫ (ЭТОТ ПРОЕКТ)

| Запрещено | Причина |
|-----------|---------|
| Пропускать `ON CONFLICT DO NOTHING` | Kafka ретраи вызывают дубликаты |
| Скриншот до загрузки ассетов | `gochrome`: ждать `Image.onload` промис |
| Хардкод версий | Использовать git теги через build args |
| Пустые healthcheck'и | Проверять ВСЕ downstream зависимости |
| Принимать cookie-диалоги | Скрывать через CSS `!important`, не кликать |
| Дефолтный `TAG` в скриптах или compose | Откатывает прод на старую версию молча — уже был реальный инцидент (`AUDIT.md`, P0-1) |
| Одинаковый порог eligibility и интервал тикера | Сообщение, обработка которого заняла ровно один тик, забирается повторно — дублирует посты в Telegram (`internal/common/queue.StaleThreshold`) |

## РЕЖИМЫ IMAGE FETCHER

`GAME_IMAGE_FETCHER_TYPE` в `.env`:

| Режим | Механизм | Когда использовать |
|-------|----------|-------------------|
| `gochrome` | chromedp: полный рендер страницы + скриншот | Используется в проде — качество важнее ресурсов, осознанный выбор при планировании v5.0 |
| `headless` | Нативная отрисовка через `fogleman/gg` по данным API, без браузера | Дефолт в коде, когда `GAME_IMAGE_FETCHER_TYPE` не задан, но в проде не используется; запасной вариант — быстрее и легче gochrome |

`selenium` удалён в v5.0 вместе с профилем compose `selenium`: код был мёртвым — профиль ни разу не включался в проде.

**Критично для gochrome**: Фоновые изображения должны быть предзагружены через JS `Image.onload` перед скриншотом.

## КОМАНДЫ

```bash
# Запуск всех сервисов
docker compose up -d

# С мониторингом (Grafana + Loki + Promtail, по умолчанию выключен)
COMPOSE_PROFILES=monitoring docker compose up -d

# Логи сервиса
docker compose logs -f <service>

# Проверка health (порт 8080; у admin-panel — 8081)
docker compose exec <service> wget -qO- http://127.0.0.1:8080/health

# Чтение топика (брокер — Redpanda, топики и переменные всё ещё называются KAFKA_*)
docker compose exec redpanda rpk topic consume <topic> --brokers redpanda:9092

# Загрузка данных расписания
docker compose exec postgres psql -U megagames -d megagames < discord_tools/sql/schema.sql
```

## РЕЛИЗЫ

```bash
TAG=5.0.0 ./scripts/release.sh

# Или вручную
git tag -a v5.0.0 -m "Release v5.0.0"
git push origin v5.0.0

# Или через GitHub CLI
gh release create v5.0.0 --title "5.0.0" --notes "..."
```

Предпочтительный путь релиза:
- commit/push в GitHub
- git tag `vX.Y.Z`
- GitHub Actions публикует multi-arch образы в GHCR
- Raspberry Pi и обычные серверы делают только `docker compose pull` и `docker compose up -d`

Не делать по умолчанию:
- не билдить релизные образы прямо на Pi
- не пушить релизные образы из локальной машины, если можно использовать GHCR через GitHub Actions
- не оставлять live-only release-изменения на хосте, если они могут быть оформлены через git + GitHub Actions

### Деплой на Pi

Источник истины для runtime-образов:
- GitHub Actions workflow `.github/workflows/publish-images.yml`
- registry: `ghcr.io/colzphml/mega_games/<service>`
- cleanup workflow `.github/workflows/cleanup-ghcr-ephemeral-tags.yml` удаляет старые `sha-*` теги

Текущий хост и путь:
- хост: `pi`
- директория проекта: `/home/colz/envs/mega_games`

Что должно быть на Pi:
- `docker-compose.yml` должен ссылаться на `${IMAGE_REGISTRY}/${IMAGE_NAMESPACE}/...:${TAG}`
- `.env` должен задавать `IMAGE_REGISTRY=ghcr.io`
- `.env` должен задавать `IMAGE_NAMESPACE=colzphml/mega_games`
- для штатного релиза `TAG` должен быть semver, например `5.0.0`
- временно допустим branch tag `release-ghcr-multiarch-actions`, если релизный git tag ещё не опубликован

Штатный порядок деплоя:

```bash
ssh pi '
  set -euo pipefail
  cd /home/colz/envs/mega_games
  export IMAGE_REGISTRY=ghcr.io
  export IMAGE_NAMESPACE=colzphml/mega_games
  export TAG=5.0.0
  docker compose pull
  docker compose up -d --force-recreate --remove-orphans --no-build
  docker compose ps
'
```

Проверка после деплоя:
- `docker compose ps` — все app-сервисы должны быть `healthy`
- `docker compose exec discord-kafka-listener wget -qO- http://127.0.0.1:8080/health`
- `docker compose exec admin-panel wget -qO- http://127.0.0.1:8081/health`
- при необходимости проверить, что контейнеры реально идут из `ghcr.io/colzphml/mega_games/...`

Guardrails для live-хоста:
- перед прямыми правками на Pi делать backup как минимум `.env` и `docker-compose.yml`
- не затирать несвязанные локальные правки на Pi без явной причины
- не пытаться собирать образы на Pi, если задача не про аварийный обход GHCR
- если GHCR приватный, сначала выполнить `docker login ghcr.io`

## ЗАМЕТКИ

- **Тесты**: пирамида из двух слоёв. Unit-тесты без внешних зависимостей
  (парсер Discord-сообщений, форматтер недели, классификация ошибок Discord,
  markdown-экранирование, eligibility-пороги, Basic Auth админки и др.) и
  интеграционные на testcontainers — Postgres через `internal/common/pgtest`,
  Redpanda через `internal/common/brokertest`, плюс MinIO. Интеграционные
  пропускают себя в `-short`-режиме через `testing.Short()`. Быстрый прогон
  без Docker: `go test ./... -short`. Полный прогон (нужен запущенный Docker):
  `go test ./...`
- **Timezone**: Все сервисы учитывают `TZ` env, по умолчанию `Europe/Moscow`
- **Autoheal**: Контейнеры автоперезапускаются при unhealthy статусе
- **Backfill**: Listener может добрать пропущенные сообщения при реконнекте через `DISCORD_BACKFILL_*`
