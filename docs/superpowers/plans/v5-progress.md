# v5.0 — Прогресс

Обновляется после каждой завершённой задачи. Источник истины по состоянию работ.

**Ветка:** `release-5.0` · **Спека:** `docs/superpowers/specs/2026-07-28-mega-games-v5-design.md`
**План:** `docs/superpowers/plans/2026-07-28-mega-games-v5.md` (+ `-part2`, `-part3`)

| Задача | Статус | Ветка worktree | Смержено |
|---|---|---|---|
| V5-01 Единый Go-модуль | ✅ | `v5/01-single-module` | 8939a9c |
| V5-02 common/config | ✅ | `v5/02-common-config` | c537319 |
| V5-03 common/retry | ✅ | `v5/03-common-retry` | c1db5aa |
| V5-04 common/health | ✅ | `v5/04-common-health` | a30ef84 |
| V5-05 common/queue | ✅ | `v5/05-common-queue` | b8ca663 |
| V5-06 Харнесс pgtest | ✅ | `v5/06-pgtest` | 5dfb47d |
| V5-07 Фикс дублей в TG | ✅ | `v5/07-eligibility` | 60a8ece |
| V5-08 Классификация ошибок Discord | ✅ | `v5/08-discord-errors` | 6c940f0 |
| V5-09 health/ready | ✅ | `v5/09-health-split` | bd747d6 |
| V5-10 Постсезон | ✅ | `v5/10-postseason` | fe8b83a |
| V5-11 Экранирование Markdown | ✅ | `v5/11-markdown-escape` | 5e99b5d |
| V5-12 Метка fallback | ✅ | `v5/12-fallback-flag` | db947b9 |
| V5-13 Мелкие фиксы и мёртвый код | ✅ | `v5/13-cleanup` | 777b077 |
| V5-14 ListPending в pgstore | ✅ | `v5/14-pgstore-listpending` | 2ccf46e |
| V5-15 Удаление MongoDB | ✅ | `v5/15-drop-mongo` | f884e11 |
| V5-16 Оптимизация gochrome | ✅ | `v5/16-chrome-reuse` | 718dee3 |
| V5-17 Окно дашборда | ✅ | `v5/17-dashboard-window` | 23cda0c |
| V5-18 Интерфейсы | ✅ | `v5/18-interfaces` | 924af6b |
| V5-19 Тесты Telegram | ✅ | `v5/19-telegram-tests` | 9d7ba9d |
| V5-20 Тесты Discord | ✅ | `v5/20-discord-tests` | 13cb354 |
| V5-21 Тесты NeonSportz | ✅ | `v5/21-neonsportz-tests` | 9b5f0f8 |
| V5-22 Тесты MinIO/Redpanda | ✅ | `v5/22-storage-tests` | eb20946 |
| V5-23 Redpanda | ✅ | `v5/23-redpanda` | 7ace3e5 |
| V5-24 Лимиты и логи | ⬜ | `v5/24-limits` | — |
| V5-25 Безопасность | ⬜ | `v5/25-security` | — |
| V5-26 Обязательный TAG | ⬜ | `v5/26-tag-required` | — |
| V5-27 Конфигурация и хост | ⬜ | `v5/27-config` | — |
| V5-28 Документация для агентов | ⬜ | `v5/28-agent-docs` | — |
| V5-29 README и AGENTS.md | ⬜ | `v5/29-docs` | — |
| V5-30 Миграция и выкатка | ⬜ | `v5/30-release` | — |
| V5-31 Устойчивость headless | ✅ | `v5/31-headless-resilience` | 06726c9 |

Легенда: ⬜ не начата · 🟡 в работе · ✅ смержена · ❌ заблокирована

## Журнал решений по ходу работ

Сюда записывается всё, что отклонилось от плана, с причиной.

| Дата | Задача | Отклонение | Причина |
|---|---|---|---|
| 2026-07-28 | setup | `v5-progress.md` создан до старта, а не в V5-28 | Global Constraints требуют обновлять трекер после каждой задачи — файл должен существовать с самого начала |
