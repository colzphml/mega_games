# БАЗА ЗНАНИЙ ПРОЕКТА

**Сгенерировано:** 2026-01-24
**Коммит:** bfa3c17
**Ветка:** release-4.0

## ОБЗОР

Go-микросервисы: Discord -> Kafka -> Обработка -> Telegram. Событийная архитектура с polyglot persistence (Postgres, MongoDB, MinIO).

## СТРУКТУРА

```
mega_games/
├── discord_kafka_listener/       # Discord -> Kafka (ID сообщений)
├── discord_kafka_processor/      # Парсинг недель/игр из Discord
├── discord_kafka_week_formatter/ # Форматирование текста недели для Telegram
├── discord_kafka_game_image/     # Генерация картинок игр (gochrome/selenium/headless)
├── discord_kafka_telegram_week_sender/   # Отправка текста недели в Telegram
├── discord_kafka_telegram_game_sender/   # Отправка картинок в Telegram
├── discord_tools/                # Утилиты: message-dump, csv-to-sql
├── monitoring/                   # Grafana + Loki + Promtail конфиги
├── scripts/                      # install.sh для установки через curl
├── docker-compose.yml            # Оркестрация всего стека
└── go.mod                        # Корневой модуль (Go 1.24)
```

Каждый сервис: `cmd/<name>/main.go` + пакеты в `internal/`.

## ГДЕ ИСКАТЬ

| Задача | Расположение | Примечания |
|--------|--------------|------------|
| Добавить Kafka topic | `docker-compose.yml` (kafka-init) + конфиги сервисов | Добавить в цикл entrypoint |
| Изменить парсинг сообщений | `discord_kafka_processor/internal/parser/` | Regex-based |
| Сменить image fetcher | `discord_kafka_game_image/internal/middle/` | `gochrome/`, `selenium/`, `headless/` |
| Добавить таблицу Postgres | `internal/store/store.go` сервиса | Автомиграция через `EnsureSchema()` |
| Формат сообщений Telegram | `*_telegram_*_sender/internal/telegram/client.go` | |
| Переменные окружения | `.env.example` | Вся конфигурация через env |
| Health сервиса | Каждый `main.go` имеет `/health` | Проверяет все зависимости |

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
| Postgres | Статусы обработки, данные расписания | Все сервисы |
| MongoDB | Метаданные изображений, коллекция game_images | Только game_image |
| MinIO | Файлы изображений (S3-совместимый) | game_image, telegram_game_sender |
| Kafka | Событийный обмен между сервисами | Все сервисы |

## СОГЛАШЕНИЯ

### Стиль кода
- Логирование: `github.com/rs/zerolog` — структурированный JSON
- Конфигурация: `internal/config/config.go` — env через `github.com/caarlos0/env`
- Нет явного линтера — использовать `go fmt`, `go vet`

### Паттерн сервисов
- Каждый сервис: healthcheck на `:8080/health`
- Graceful shutdown через `signal.NotifyContext`
- Retry с exponential backoff для внешних зависимостей
- Функции `waitFor*` блокируют до готовности зависимости

### База данных
- **Идемпотентность обязательна**: все INSERT используют `ON CONFLICT DO NOTHING`
- Схема автомигрируется через `EnsureSchema()` при старте
- Статусы: `new` -> `processed` | `failed`

### Docker
- Multi-stage сборка: `golang:1.25.5-alpine` -> `alpine:3.21`
- Версия через `-ldflags` из `git describe --tags --always`
- Non-root пользователь (`app`) в финальном образе
- Все сервисы с лейблом `autoheal=true`

## АНТИПАТТЕРНЫ (ЭТОТ ПРОЕКТ)

| Запрещено | Причина |
|-----------|---------|
| Пропускать `ON CONFLICT DO NOTHING` | Kafka ретраи вызывают дубликаты |
| Скриншот до загрузки ассетов | `gochrome`: ждать `Image.onload` промис |
| Хардкод версий | Использовать git теги через build args |
| Пустые healthcheck'и | Проверять ВСЕ downstream зависимости |
| Принимать cookie-диалоги | Скрывать через CSS `!important`, не кликать |

## РЕЖИМЫ IMAGE FETCHER

`GAME_IMAGE_FETCHER_TYPE` в `.env`:

| Режим | Механизм | Когда использовать |
|-------|----------|-------------------|
| `headless` | API-генерация, без браузера | Быстро, простые картинки |
| `gochrome` | chromedp скриншот | По умолчанию, полный рендер страницы |
| `selenium` | Remote Selenium WebDriver | Требует `COMPOSE_PROFILES=selenium` |

**Критично для gochrome**: Фоновые изображения должны быть предзагружены через JS `Image.onload` перед скриншотом.

## КОМАНДЫ

```bash
# Запуск всех сервисов
docker compose up -d

# С Selenium
COMPOSE_PROFILES=selenium docker compose up -d

# Логи сервиса
docker compose logs -f <service>

# Проверка health
docker compose exec <service> wget -qO- http://127.0.0.1:8080/health

# Чтение Kafka топика
docker compose exec kafka kafka-console-consumer \
  --bootstrap-server kafka:9092 --topic <topic> --from-beginning

# Загрузка данных расписания
docker compose exec postgres psql -U megagames -d megagames < discord_tools/sql/schema.sql
```

## РЕЛИЗЫ

```bash
TAG=4.0.0 ./scripts/release.sh

# Или вручную
git tag -a v4.0.0 -m "Release v4.0.0"
git push origin v4.0.0

# Или через GitHub CLI
gh release create v4.0.0 --title "4.0.0" --notes "..."
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

## ЗАМЕТКИ

- **Нет unit-тестов**: Проект использует healthcheck'и + статусные таблицы для валидации
- **Timezone**: Все сервисы учитывают `TZ` env, по умолчанию `Europe/Moscow`
- **Autoheal**: Контейнеры автоперезапускаются при unhealthy статусе
- **Backfill**: Listener может добрать пропущенные сообщения при реконнекте через `DISCORD_BACKFILL_*`
