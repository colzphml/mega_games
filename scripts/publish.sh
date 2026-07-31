#!/bin/bash
set -e

# No default: the previous 4.3.2 fallback would have built and pushed
# a seven-release-old image under a stale tag if TAG was left unset.
if [[ -z "${TAG:-}" ]]; then
  printf 'ERROR: TAG is required, e.g. TAG=5.0.0 %s\n' "$0" >&2
  exit 1
fi

export TAG
export IMAGE_REGISTRY="${IMAGE_REGISTRY:-ghcr.io}"
export IMAGE_NAMESPACE="${IMAGE_NAMESPACE:-colzphml/mega_games}"

echo "Building images with tag: $TAG"
docker compose build

echo "Pushing images to registry..."
docker compose push

echo "Done! Images pushed to ${IMAGE_REGISTRY}/${IMAGE_NAMESPACE}/*:$TAG"
echo "Preferred release path: TAG=$TAG ./scripts/release.sh"
