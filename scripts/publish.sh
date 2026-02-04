#!/bin/bash
set -e

# Default tag if not set
export TAG="${TAG:-4.1.0}"

echo "Building images with tag: $TAG"
docker compose build

echo "Pushing images to registry..."
docker compose push

echo "Done! Images pushed to 192.168.0.61:5000/mega_games/*:$TAG"
