#!/usr/bin/env bash
# Host-level tuning for the Raspberry Pi. Idempotent: running it again on an
# already-tuned host re-applies the same settings without duplicating
# anything (see the cron section below for the part that needs care).
#
#   1. Lower vm.swappiness from the Debian default (60) to 10.
#   2. Reclaim disk space from old, unused Docker images (one-time, runs now).
#   3. Install a weekly cron job that repeats step 2 going forward.
set -euo pipefail

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

if [[ "$(uname -s)" != "Linux" ]]; then
  die "this script targets the Raspberry Pi host (Linux); refusing to run on $(uname -s)"
fi

echo "==> swappiness"
# The Pi has 3796 MB of RAM with roughly 2.5 GB typically available, so
# there is no memory shortage. But the default swappiness=60 still makes
# the kernel evict pages to the SD card under everyday memory pressure,
# which is both slow (an SD card is not an SSD) and wears the card out
# over time. Ten keeps swap available as a last resort instead of a first
# response. Writing the same drop-in file again is a no-op.
sudo tee /etc/sysctl.d/60-mega-games.conf >/dev/null <<'EOF'
vm.swappiness=10
EOF
sudo sysctl --system >/dev/null
echo "    vm.swappiness = $(cat /proc/sys/vm/swappiness)"

echo "==> docker image cleanup (one-time)"
# Every deploy that changes an image tag leaves the previous tag's image
# behind, unreferenced by any container. These accumulate: the game-image
# service alone bundles a ~700 MB Chromium, so a handful of old versions
# add up fast (17.5 GB total observed on this host, 13.6 GB of it reclaimable
# this way). -a considers tagged-but-unused images too, not just dangling
# ones; the 336h (14 day) filter leaves anything from a very recent deploy
# alone in case it's still needed for a quick rollback. Safe to repeat: a
# clean host just reports nothing to reclaim.
docker image prune -a -f --filter "until=336h"
docker builder prune -f

echo "==> weekly cleanup cron"
# Reinstalling the crontab is the part that actually needs to be idempotent
# on purpose, not just harmless-by-accident: every run replaces any prior
# copy of this job with exactly one fresh copy, instead of appending a
# duplicate each time.
#
# Both of the following are *expected* to report failure the first time
# this runs, and that must not abort the script or wipe the crontab:
#   - `crontab -l` exits 1 with "no crontab for <user>" before this script
#     has ever installed one (e.g. a freshly imaged Pi).
#   - `grep -v` exits 1 when it filters out every line it was given - which
#     happens on the second and every later run, once this job is the only
#     line in the crontab.
# Without the `|| true` guards, set -e/pipefail would abort the pipeline
# right there, before the new line is ever printed, and `crontab -` would
# then be fed empty input - installing an empty crontab (silently dropping
# both this job and anything else the user had) instead of the intended
# one-line update.
CRON_LINE='0 4 * * 0 docker image prune -a -f --filter "until=336h" >/dev/null 2>&1'
existing_cron="$(crontab -l 2>/dev/null || true)"
kept_cron="$(printf '%s\n' "$existing_cron" | grep -v 'docker image prune' || true)"
{
  if [[ -n "$kept_cron" ]]; then
    printf '%s\n' "$kept_cron"
  fi
  printf '%s\n' "$CRON_LINE"
} | crontab -
echo "    installed: $CRON_LINE"

echo "==> disk"
df -h / | tail -1

echo "Done."
