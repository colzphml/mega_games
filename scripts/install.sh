#!/usr/bin/env bash
set -euo pipefail

REPO_URL="https://github.com/colzphml/mega_games.git"
DEFAULT_TAG="v4.2.0"
DEFAULT_INSTALL_DIR="/opt/mega_games"

TAG="${TAG:-$DEFAULT_TAG}"
INSTALL_DIR="${INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"
NONINTERACTIVE="${NONINTERACTIVE:-0}"
PROMPT_INPUT=""

if [[ -r /dev/tty ]]; then
  PROMPT_INPUT="/dev/tty"
fi

log() {
  printf '%s\n' "$*"
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

confirm() {
  local prompt="$1"
  local default="${2:-Y}"
  local reply
  if [[ "$NONINTERACTIVE" == "1" ]]; then
    [[ "$default" == "Y" ]] && return 0 || return 1
  fi
  if [[ "$default" == "Y" ]]; then
    prompt="$prompt [Y/n]"
  else
    prompt="$prompt [y/N]"
  fi
  if [[ -n "$PROMPT_INPUT" ]]; then
    read -r -p "$prompt " reply < "$PROMPT_INPUT" || true
  else
    read -r -p "$prompt " reply || true
  fi
  case "$reply" in
    [Yy]*) return 0 ;;
    [Nn]*) return 1 ;;
    "") [[ "$default" == "Y" ]] && return 0 || return 1 ;;
    *) return 1 ;;
  esac
}

set_env() {
  local key="$1"
  local value="$2"
  local file="$3"
  local escaped
  escaped="${value//\\/\\\\}"
  escaped="${escaped//&/\\&}"
  escaped="${escaped//|/\\|}"

  if grep -q "^${key}=" "$file"; then
    if [[ "$OS" == "Darwin" ]]; then
      sed -i '' "s|^${key}=.*|${key}=${escaped}|" "$file"
    else
      sed -i "s|^${key}=.*|${key}=${escaped}|" "$file"
    fi
  else
    printf '%s=%s\n' "$key" "$value" >> "$file"
  fi
}

set_env_if_empty() {
  local key="$1"
  local value="$2"
  local file="$3"
  local current
  current=$(grep -E "^${key}=" "$file" | head -n1 | cut -d= -f2- || true)
  if [[ -z "$current" ]]; then
    set_env "$key" "$value" "$file"
  fi
}

OS="$(uname -s)"
case "$OS" in
  Linux|Darwin) ;;
  *) die "unsupported OS: $OS" ;;
esac

ARCH_RAW="$(uname -m)"
case "$ARCH_RAW" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  armv7l|armv6l) die "unsupported arch: $ARCH_RAW (need 64-bit OS)" ;;
  *) die "unsupported arch: $ARCH_RAW" ;;
esac

require_cmd curl

if command -v git >/dev/null 2>&1; then
  HAS_GIT=1
else
  HAS_GIT=0
  require_cmd tar
fi

if command -v docker >/dev/null 2>&1; then
  if docker info >/dev/null 2>&1; then
    DOCKER="docker"
  elif command -v sudo >/dev/null 2>&1; then
    DOCKER="sudo docker"
  else
    die "docker requires sudo or user is not in docker group"
  fi
else
  die "docker is not installed"
fi

if $DOCKER compose version >/dev/null 2>&1; then
  COMPOSE=("$DOCKER" compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE=(docker-compose)
else
  die "docker compose is not available"
fi

SUDO=""
if [[ ! -d "$INSTALL_DIR" ]]; then
  if [[ ! -w "$(dirname "$INSTALL_DIR")" ]]; then
    if command -v sudo >/dev/null 2>&1; then
      SUDO="sudo"
    else
      die "need sudo to create $INSTALL_DIR"
    fi
  fi
  $SUDO mkdir -p "$INSTALL_DIR"
fi

if [[ -n "$SUDO" ]]; then
  $SUDO chown -R "$(id -u)":"$(id -g)" "$INSTALL_DIR"
fi

if [[ $HAS_GIT -eq 1 && -d "$INSTALL_DIR/.git" ]]; then
  log "Updating existing repo in $INSTALL_DIR"
  git -C "$INSTALL_DIR" fetch --tags
  git -C "$INSTALL_DIR" checkout "$TAG"
  git -C "$INSTALL_DIR" reset --hard "$TAG"
elif [[ $HAS_GIT -eq 1 ]]; then
  if [[ -n "$(ls -A "$INSTALL_DIR" 2>/dev/null || true)" ]]; then
    die "install dir is not empty: $INSTALL_DIR"
  fi
  log "Cloning $REPO_URL ($TAG) into $INSTALL_DIR"
  git clone --branch "$TAG" --depth 1 "$REPO_URL" "$INSTALL_DIR"
else
  if [[ -n "$(ls -A "$INSTALL_DIR" 2>/dev/null || true)" ]]; then
    die "install dir is not empty: $INSTALL_DIR"
  fi
  TARBALL_URL="https://github.com/colzphml/mega_games/archive/refs/tags/${TAG}.tar.gz"
  TMP_DIR="$(mktemp -d)"
  log "Downloading $TARBALL_URL"
  curl -fsSL "$TARBALL_URL" -o "$TMP_DIR/repo.tgz"
  tar -xzf "$TMP_DIR/repo.tgz" -C "$INSTALL_DIR" --strip-components=1
  rm -rf "$TMP_DIR"
fi

ENV_FILE="$INSTALL_DIR/.env"
EXAMPLE_FILE="$INSTALL_DIR/.env.example"

if [[ ! -f "$ENV_FILE" ]]; then
  if [[ "$NONINTERACTIVE" == "1" ]]; then
    cp "$EXAMPLE_FILE" "$ENV_FILE"
  else
    if confirm "Create .env from .env.example?" "Y"; then
      cp "$EXAMPLE_FILE" "$ENV_FILE"
    else
      die "missing .env; cannot continue"
    fi
  fi
fi

if [[ "$NONINTERACTIVE" != "1" ]] && confirm "Configure .env interactively now?" "Y"; then
  if [[ -n "$PROMPT_INPUT" ]]; then
    read -r -p "DISCORD_TOKEN (leave empty to skip): " v < "$PROMPT_INPUT" || true
  else
    read -r -p "DISCORD_TOKEN (leave empty to skip): " v || true
  fi
  [[ -n "$v" ]] && set_env "DISCORD_TOKEN" "$v" "$ENV_FILE"

  if [[ -n "$PROMPT_INPUT" ]]; then
    read -r -p "DISCORD_CHANNEL_ID (leave empty to skip): " v < "$PROMPT_INPUT" || true
  else
    read -r -p "DISCORD_CHANNEL_ID (leave empty to skip): " v || true
  fi
  [[ -n "$v" ]] && set_env "DISCORD_CHANNEL_ID" "$v" "$ENV_FILE"

  if [[ -n "$PROMPT_INPUT" ]]; then
    read -r -p "TELEGRAM_BOT_TOKEN (leave empty to skip): " v < "$PROMPT_INPUT" || true
  else
    read -r -p "TELEGRAM_BOT_TOKEN (leave empty to skip): " v || true
  fi
  [[ -n "$v" ]] && set_env "TELEGRAM_BOT_TOKEN" "$v" "$ENV_FILE"

  if [[ -n "$PROMPT_INPUT" ]]; then
    read -r -p "TELEGRAM_WEEK_CHAT_ID (leave empty to skip): " v < "$PROMPT_INPUT" || true
  else
    read -r -p "TELEGRAM_WEEK_CHAT_ID (leave empty to skip): " v || true
  fi
  [[ -n "$v" ]] && set_env "TELEGRAM_WEEK_CHAT_ID" "$v" "$ENV_FILE"

  if [[ -n "$PROMPT_INPUT" ]]; then
    read -r -p "TELEGRAM_GAME_CHAT_ID (leave empty to skip): " v < "$PROMPT_INPUT" || true
  else
    read -r -p "TELEGRAM_GAME_CHAT_ID (leave empty to skip): " v || true
  fi
  [[ -n "$v" ]] && set_env "TELEGRAM_GAME_CHAT_ID" "$v" "$ENV_FILE"

  if [[ -n "$PROMPT_INPUT" ]]; then
    read -r -p "GAME_IMAGE_FETCHER_TYPE (headless|gochrome|selenium) [headless]: " v < "$PROMPT_INPUT" || true
  else
    read -r -p "GAME_IMAGE_FETCHER_TYPE (headless|gochrome|selenium) [headless]: " v || true
  fi
  if [[ -n "$v" ]]; then
    set_env "GAME_IMAGE_FETCHER_TYPE" "$v" "$ENV_FILE"
  fi

  if confirm "Enable selenium profile?" "N"; then
    set_env "COMPOSE_PROFILES" "selenium" "$ENV_FILE"
  fi
fi

TARGET_PLATFORM_VALUE="${TARGET_PLATFORM:-linux/${ARCH}}"
set_env "TARGET_PLATFORM" "$TARGET_PLATFORM_VALUE" "$ENV_FILE"

log "Starting services..."
cd "$INSTALL_DIR"
"${COMPOSE[@]}" up -d --build

log "Done. Compose file: $INSTALL_DIR/docker-compose.yml"
