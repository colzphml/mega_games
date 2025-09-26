#!/bin/bash

# Логирование
LOG_FILE="/var/log/mega_games_restart.log"

# Функция для логирования
log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') - $1" >> "$LOG_FILE"
}

log "Начинаем перезапуск mega_games Docker контейнеров"

# Переход в рабочую директорию
cd /opt/mega_games

# Проверка существования docker-compose файла
if [ ! -f "docker-compose.yml" ] && [ ! -f "compose.yml" ]; then
    log "ОШИБКА: Файл docker-compose.yml или compose.yml не найден в /opt/mega_games"
    exit 1
fi

# Перезапуск контейнеров
log "Выполняем docker compose restart"
docker compose restart

# Проверка статуса
if [ $? -eq 0 ]; then
    log "Контейнеры успешно перезапущены"
else
    log "ОШИБКА: Ошибка при перезапуске контейнеров"
    exit 1
fi

log "Перезапуск завершен"
