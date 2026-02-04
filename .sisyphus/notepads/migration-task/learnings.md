## Database Migration for Schedule Constraints

- Implemented deduplication and unique index for 'schedule_games' to ensure data integrity.
- Migration file created at 'discord_tools/sql/migration_001_schedule_constraint.sql'.
- Updated 'EnsureSchema' in 'discord_kafka_week_formatter/internal/store/store.go' to run dedupe before index creation.
- Updated 'discord_tools/sql/schema.sql' for fresh installs.
