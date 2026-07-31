#!/bin/bash
set -euo pipefail

# No default: the previous 4.3.2 fallback would have rolled production
# back seven releases, losing the NeonSportz selector and VPN timeout
# fixes, if anyone ran this without TAG set.
if [[ -z "${TAG:-}" ]]; then
  printf 'ERROR: TAG is required, e.g. TAG=5.0.0 %s\n' "$0" >&2
  exit 1
fi

export IMAGE_REGISTRY="${IMAGE_REGISTRY:-ghcr.io}"
export IMAGE_NAMESPACE="${IMAGE_NAMESPACE:-colzphml/mega_games}"
export TAG

echo "Deploying tag: $TAG"
echo "Registry: $IMAGE_REGISTRY/$IMAGE_NAMESPACE"

echo "Validating compose configuration..."
docker compose config >/dev/null

echo "Pulling images from registry..."
docker compose pull

echo "Recreating services without local builds..."
docker compose up -d --force-recreate --remove-orphans --no-build

echo "Current service state:"
docker compose ps

echo "Deployment complete."
