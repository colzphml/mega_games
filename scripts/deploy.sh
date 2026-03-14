#!/bin/bash
set -e

# Default tag if not set
export TAG="${TAG:-4.3.0}"

echo "Deploying version: $TAG"

echo "Attempting to pull images from registry..."
# Try to pull. If images are missing in registry, this will fail for those images.
# We use --ignore-pull-failures to continue even if some images are missing.
docker compose pull --ignore-pull-failures

echo "Starting services..."
# 'up' will use the pulled image if available.
# If the image was not pulled (missing in registry) and not found locally, 
# Docker Compose will build it using the 'build' context defined in docker-compose.yml.
docker compose up -d --remove-orphans

echo "Deployment complete."
