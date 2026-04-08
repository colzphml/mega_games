#!/usr/bin/env bash
set -euo pipefail

TAG_INPUT="${TAG:-}"

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

if [[ -z "${TAG_INPUT}" ]]; then
  die "set TAG=vX.Y.Z or TAG=X.Y.Z"
fi

if [[ "${TAG_INPUT}" == v* ]]; then
  RELEASE_TAG="${TAG_INPUT}"
else
  RELEASE_TAG="v${TAG_INPUT}"
fi

git diff --quiet || die "working tree has unstaged changes"
git diff --cached --quiet || die "working tree has staged but uncommitted changes"

git fetch --tags origin

if git rev-parse "${RELEASE_TAG}" >/dev/null 2>&1; then
  die "tag already exists locally: ${RELEASE_TAG}"
fi

if git ls-remote --exit-code --tags origin "refs/tags/${RELEASE_TAG}" >/dev/null 2>&1; then
  die "tag already exists on origin: ${RELEASE_TAG}"
fi

git tag -a "${RELEASE_TAG}" -m "Release ${RELEASE_TAG}"
git push origin "${RELEASE_TAG}"

printf 'Pushed %s. GitHub Actions will publish images to GHCR.\n' "${RELEASE_TAG}"
