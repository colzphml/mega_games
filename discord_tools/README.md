# Discord Tools

Набор утилит для разовых операций с Discord и локальными данными.

## Требования
- Go 1.25+
- Переменные окружения для Discord:
  - `DISCORD_TOKEN`
  - `DISCORD_CHANNEL_ID`

## Утилиты

### 1) Получить JSON сообщения по ID

Команда:
```bash
cd discord_tools
DISCORD_TOKEN=... DISCORD_CHANNEL_ID=... go run ./cmd/message-dump/main.go 143123412341234
```

Результат: полный JSON сообщения (включая embeds/fields) в stdout.

### 2) Сгенерировать SQL из CSV (teams + schedule)

Команда:
```bash
cd discord_tools
go run ./cmd/csv-to-sql --teams ../MEGA_teams.csv --schedule ../MEGA_games.csv > seed.sql
```

Что делает:
- Читает CSV с командами и расписанием.
- Генерирует SQL INSERT-скрипт.

Расписание читается по заголовкам `homeTeam`, `awayTeam`, `seasonIndex`,
`stageIndex`, `weekIndex`: порядок колонок и дополнительные поля (например,
`gotw_reason`) могут меняться. Индексы должны быть неотрицательными целыми числами, команды —
непустыми. Нулевой `seasonIndex` допустим. Отсутствующие или повторные заголовки
и некорректные строки завершают импорт с ошибкой; пустое расписание не принимается.
Для строки указывается её номер
и проблемное поле. При ошибке SQL не выводится.

Загрузка `MEGA_games.csv` в разделе Schedule админ-панели использует те же правила.
Весь CSV проверяется до записи игр в базу; некорректный файл получает HTTP 400.

Как применить:
1) Сначала создайте таблицы из `sql/schema.sql` в своей базе.
2) Затем выполните `seed.sql` через DBeaver.

## Примеры

### Пример: получить JSON сообщения
```bash
DISCORD_TOKEN=abc DISCORD_CHANNEL_ID=123 go run ./cmd/message-dump/main.go 1461458110987370582
```

### Пример: подготовить SQL для базы
```bash
go run ./cmd/csv-to-sql --teams ../MEGA_teams.csv --schedule ../MEGA_games.csv > seed.sql
```

## Деплой и доступы

`discord_tools` не требует отдельного контейнера и запускается локально как утилита.
Для release/deploy образов сервисов, URL-ов (admin/images/grafana) и команд обновления на Raspberry см. корневой `README.md`.
