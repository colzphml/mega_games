#!/bin/bash
set -euo pipefail

export IMAGE_REGISTRY="${IMAGE_REGISTRY:-ghcr.io}"
export IMAGE_NAMESPACE="${IMAGE_NAMESPACE:-colzphml/mega_games}"
export TAG="${TAG:-4.3.2}"

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
