#!/usr/bin/env bash
# Prints the counts that must not change across the v4 -> v5 migration.
#
# Run it before the migration and again after; the two outputs have to be
# identical, byte for byte. Anything else means history was lost and the
# migration must be rolled back rather than investigated in place.
#
# The script only ever reads. It works against both the old stack (Kafka,
# Zookeeper, MongoDB) and the new one, because the tables and the volumes
# it looks at are the same in both -- only the broker changed.
#
# It fails loudly when a count cannot be taken. That matters more than any
# individual number here: a comparison of two silent failures looks exactly
# like a comparison of two matching results, and would hand us a false
# all-clear at the one moment we care most.
set -euo pipefail

# Normally the deployment directory is the repository this script lives in.
# DEPLOY_DIR overrides that, so the same script can be run from outside the
# checkout -- which is how the pre-migration baseline is taken, before the
# host has switched branches and while the script is not in its tree yet.
cd "${DEPLOY_DIR:-$(dirname "$0")/..}"

die() {
  printf 'verify-history: %s\n' "$*" >&2
  exit 1
}

command -v docker >/dev/null 2>&1 || die "docker not found in PATH"

docker compose ps postgres >/dev/null 2>&1 ||
  die "cannot talk to compose -- is this the deployment directory?"

PG_USER="${POSTGRES_USER:-megagames}"
PG_DB="${POSTGRES_DB:-megagames}"

# -A -t -F' ' keeps the output stable: unaligned, no header, no row count,
# single space between the label and the number. Nothing here varies
# between runs, so a diff of two runs shows real changes only.
sql_counts=$(docker compose exec -T postgres \
  psql -U "$PG_USER" -d "$PG_DB" -A -t -F' ' <<'SQL'
SELECT 'discord_message_status', count(*) FROM discord_message_status
UNION ALL SELECT 'discord_message_failed', count(*) FROM discord_message_failed
UNION ALL SELECT 'week_message_status', count(*) FROM week_message_status
UNION ALL SELECT 'week_message_failed', count(*) FROM week_message_failed
UNION ALL SELECT 'game_image_status', count(*) FROM game_image_status
UNION ALL SELECT 'game_image_failed', count(*) FROM game_image_failed
UNION ALL SELECT 'game_image_with_url', count(*) FROM game_image_status
    WHERE image->>'image_url' IS NOT NULL AND image->>'image_url' <> ''
UNION ALL SELECT 'telegram_week_status', count(*) FROM telegram_week_status
UNION ALL SELECT 'telegram_week_failed', count(*) FROM telegram_week_failed
UNION ALL SELECT 'telegram_game_status', count(*) FROM telegram_game_status
UNION ALL SELECT 'telegram_game_failed', count(*) FROM telegram_game_failed
UNION ALL SELECT 'teams', count(*) FROM teams
UNION ALL SELECT 'schedule_games', count(*) FROM schedule_games
ORDER BY 1;
SQL
) || die "postgres query failed -- refusing to print a partial baseline"

[[ -n "$sql_counts" ]] || die "postgres returned nothing"

printf '%s\n' "$sql_counts"

# MinIO ships no find and no grep, so the object count is taken with mc,
# which is in the image. "local" is mc's built-in alias for the server it
# runs inside, so no credentials or alias setup are needed.
bucket="${MINIO_BUCKET:-game-images}"

minio_objects=$(docker compose exec -T minio \
  mc ls --recursive "local/${bucket}" 2>/dev/null | wc -l | tr -d ' ') ||
  die "could not list the minio bucket '${bucket}'"

[[ -n "$minio_objects" ]] || die "minio object count came back empty"

printf 'minio_objects %s\n' "$minio_objects"

# Size is informational: it moves with compaction and metadata, so it is
# printed for the record but is not part of what must match exactly.
minio_size=$(docker compose exec -T minio du -sh /data 2>/dev/null | cut -f1) ||
  die "could not measure the minio data directory"

printf 'minio_size_informational %s\n' "$minio_size"
