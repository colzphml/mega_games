#!/bin/bash
set -e

# Default tag if not set
export TAG="${TAG:-4.3.2}"
export IMAGE_REGISTRY="${IMAGE_REGISTRY:-ghcr.io}"
export IMAGE_NAMESPACE="${IMAGE_NAMESPACE:-colzphml/mega_games}"

echo "Building images with tag: $TAG"
docker compose build

echo "Pushing images to registry..."
docker compose push

echo "Done! Images pushed to ${IMAGE_REGISTRY}/${IMAGE_NAMESPACE}/*:$TAG"
