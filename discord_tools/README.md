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
