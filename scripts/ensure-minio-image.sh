#!/usr/bin/env bash
set -euo pipefail

# Preserve the official ARM64 image used on Pi; do not rebuild MinIO.
image='ghcr.io/colzphml/mega_games/minio:RELEASE.2025-09-07T16-13-09Z-arm64'
source_image='minio/minio:RELEASE.2025-09-07T16-13-09Z'
asset='minio-RELEASE.2025-09-07T16-13-09Z-linux-arm64.tar.gz'
checksum='b75c228530a1c0c006233097afbdcf10ba493d96684e98834b2d3a3e1e15e297'
config_digest='8f08aee614800a237906bd48114d733e5ac5bfac4ccdf731f141b0e880d7a253'

work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT

verify_image() {
  # Docker's containerd backend reports a manifest as .Id, so hash the actual
  # saved config instead. It anchors the official config and RootFS layer IDs.
  docker image save --output "$work_dir/verify.tar" "$image"
  python3 - "$work_dir/verify.tar" "$config_digest" <<'PY'
import hashlib
import json
import sys
import tarfile

with tarfile.open(sys.argv[1]) as archive:
    manifest = json.load(archive.extractfile('manifest.json'))
    configs = [archive.extractfile(item['Config']).read() for item in manifest]
    if len(configs) != 1 or hashlib.sha256(configs[0]).hexdigest() != sys.argv[2]:
        raise SystemExit('MinIO config digest differs from the official Pi image')
print('Verified official MinIO ARM64 image config')
PY
}

if docker pull --platform linux/arm64 "$image"; then
  verify_image
  exit 0
fi

# GitHub keeps a checksum-pinned recovery copy if the registry is unavailable.
curl --fail --location --retry 3 --output "$work_dir/$asset" \
  "https://github.com/colzphml/mega_games/releases/download/deps-minio-2025-09-07-arm64/$asset"
python3 - "$work_dir/$asset" "$checksum" <<'PY'
import hashlib
import sys

digest = hashlib.sha256()
with open(sys.argv[1], 'rb') as archive:
    for chunk in iter(lambda: archive.read(1024 * 1024), b''):
        digest.update(chunk)
if digest.hexdigest() != sys.argv[2]:
    raise SystemExit('MinIO recovery archive checksum mismatch')
print('Verified MinIO recovery archive checksum')
PY
docker image load --input "$work_dir/$asset"
docker image tag "$source_image" "$image"
verify_image

# PRs can use the recovery copy without needing permission to publish packages.
if [[ "${PUBLISH_MINIO_MIRROR:-false}" == 'true' ]]; then
  docker image push "$image"
fi
