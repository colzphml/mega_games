# Mega Games Result Sender

Автоматизирует передачу результатов турнира из Discord в Telegram, используя Selenium для получения скриншотов с сайта лиги. Приложение запускает три независимых сервиса (источник, промежуточный слой и целевой), объединённые по каналам обмена сообщениями.

## Что делает сервис
- Подключается к Discord-каналу и отслеживает сообщения бота с вложениями (embeds).
- Распознаёт объявления о переходе на новую неделю сезона и формирует расписание на основе CSV с играми и командами.
- Для каждого embed с ссылкой на игру открывает страницу в браузере Chrome через Selenium Hub, переключается на вкладку Recap и инициирует выгрузку `game-recap.jpeg`.
- Переименовывает скачанный файл по номеру игры, читает байты и отправляет изображение вместе с ссылкой в общий канал Telegram.
- Публикует текст объявления «новой недели» в новостной канал Telegram с дедлайном для анонсов матчей.

## Архитектура
```
Discord → Source (internal/sourceSM/discord) → Middle (internal/middle/selenium) → Target (internal/targetSM/telegram) → Telegram
                          ↘︎ utils.ParseScheduleFromCSV / ParseCSVFileToTeams (расписание и команды)
```

### Основные компоненты
- **Source (Discord)** — `internal/sourceSM/discord`: слушает новые сообщения, парсит embeds, выявляет ссылки на игры и объявления о новой неделе.
- **Middle (Selenium)** — `internal/middle/selenium`: открывает страницу игры, нажимает Download и дожидается появления файла в `app.file_storage_path`.
- **Target (Telegram)** — `internal/targetSM/telegram`: в зависимости от типа сообщения отправляет либо текст в новостной канал, либо изображение в общий канал.
- **Конфигурация** — `pkg/config`: загружает YAML и переопределяет значения переменными окружения, проверяет обязательные поля.
- **Утилиты** — `pkg/utils`: собирает расписание и команды из CSV (`MEGA_games.csv`, `MEGA_teams.csv`).

## Требования
- Go `>= 1.22`
- Docker и Docker Compose (для запуска Selenium и приложения в контейнерах)
- Файлы данных: `MEGA_games.csv`, `MEGA_teams.csv`
- Настроенный `config.yaml` с актуальными токенами и путями

## Настройка конфигурации
Создайте `config.yaml` в корне проекта. Пример:

```yaml
app:
  source:
    type: "discord"
    token: "DISCORD_BOT_TOKEN"
    channel_id: "DISCORD_CHANNEL_ID"
  middle:
    type: "selenium"
    middle_url: "http://selenium:4444/wd/hub"
  target:
    type: "telegram"
    token: "TELEGRAM_BOT_TOKEN"
    common_channel_id: "TELEGRAM_COMMON_CHAT_ID"
    news_channel_id: "TELEGRAM_NEWS_CHAT_ID"
  file_storage_path: "./screens"
  games_url: "https://neonsportz.com/leagues/MEGA/games"
  schedule_path: "./MEGA_games.csv"
  players_path: "./MEGA_teams.csv"
  deep_history: 100
  cache_size: 100
```

> Значения могут быть переопределены переменными окружения (`SOURCE_TOKEN`, `TARGET_TOKEN` и т.д.). Все поля обязательны — при отсутствии любого приложение завершится с ошибкой на этапе запуска.

## Запуск
### Через Docker Compose
```bash
docker compose up -d
```
- Сервис `app` собирается из `./cmd`
- Сервис `selenium` поднимает Chrome WebDriver
- Скачанные файлы сохраняются в `./screens`

Просмотр логов:
```bash
docker compose logs -f app
```
Остановка:
```bash
docker compose down
```

### Локально (без контейнеров)
1. Запустите Selenium Hub/Node локально или используйте существующий.
2. Убедитесь, что Chrome доступен для Selenium.
3. Выполните:
   ```bash
   go run ./cmd
   ```

## Как работает поток событий
1. Discord bot получает embed → `HandleMessages` кладёт сообщение во внутренний канал.
2. `ProceedMessages` опрашивает сообщение до появления embeds:
   - Если embed объявляет новую неделю — составляется расписание и отправляется `TargetMessage{Action:"newWeek"}`.
   - Если embed содержит ссылки на игры — формируется `DiscordGame` на каждую ссылку и отправляется в канал middle.
3. Middle получает `DiscordGame`, открывает страницу Selenium, нажимает Download и ждёт файл `game-recap.jpeg`.
4. Файл переименовывается в `<gameId>.jpeg`, читается и отправляется как `TargetMessage{Action:"game"}`.
5. Telegram клиент принимает сообщение:
   - `newWeek` → Markdown-анонс в новостной чат с дедлайном (зона `Europe/Moscow`).
   - `game` → Загрузка изображения в общий чат с подписью-ссылкой.

## Отладка и советы
- Если Selenium не может найти элементы, включите `bot.Debug = true` в Telegram и увеличьте таймауты в `interactWithPage`.
- Проверяйте, что каталог `app.file_storage_path` смонтирован в контейнер Selenium и доступен приложению.
- Для обновления расписания/команд замените CSV-файлы и перезапустите сервис.

## Структура каталогов
- `cmd/main.go` — точка входа.
- `internal/app` — сборка приложения, управление goroutine и завершением.
- `internal/sourceSM` — источники данных (Discord).
- `internal/middle` — промежуточная обработка (Selenium).
- `internal/targetSM` — отправка в целевые системы (Telegram).
- `pkg/config` — конфигурация.
- `pkg/utils` — утилиты работы с CSV.

---
Поддерживайте `config.yaml` и токены в безопасности: файл добавлен в `.gitignore`, не коммитьте секреты в репозиторий.
