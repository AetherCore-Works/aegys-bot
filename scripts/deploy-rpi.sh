#!/usr/bin/env bash
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RPI_HOST="${RPI_HOST:-rpi}"
RPI_STACK_DIR="/opt/stacks/aegys-bot"
AEGYS_PORT="${AEGYS_PORT:-8091}"

echo "==> [1/4] Verifying and testing Go backend..."
cd "$REPO_DIR"
go vet ./...
go test -count=1 ./...

echo "==> [2/4] Syncing code to ${RPI_HOST}:${RPI_STACK_DIR} (preserving .env)..."
ssh "$RPI_HOST" "mkdir -p '${RPI_STACK_DIR}'"
rsync -az --delete \
  --exclude='.git' \
  --exclude='.env' \
  --exclude='bin' \
  --exclude='config/commands.json' \
  "$REPO_DIR/" "${RPI_HOST}:${RPI_STACK_DIR}/"

echo "==> [3/4] Ensuring default .env exists on ${RPI_HOST}..."
ssh "$RPI_HOST" "if [ ! -f '${RPI_STACK_DIR}/.env' ]; then cp '${RPI_STACK_DIR}/.env.example' '${RPI_STACK_DIR}/.env' && sed -i 's|^AEGYS_PORT=.*|AEGYS_PORT=${AEGYS_PORT}|' '${RPI_STACK_DIR}/.env' && sed -i 's|^AI_BACKEND=.*|AI_BACKEND=agy-gateway|' '${RPI_STACK_DIR}/.env' && sed -i 's|^AGY_GATEWAY_URL=.*|AGY_GATEWAY_URL=http://host.docker.internal:8088|' '${RPI_STACK_DIR}/.env' && echo 'Created initial .env from .env.example'; fi"

echo "==> [4/4] Rebuilding and restarting aegys-bot container on ${RPI_HOST}..."
ssh "$RPI_HOST" "cd '${RPI_STACK_DIR}' && docker compose up -d --build && sleep 2 && curl -sf http://127.0.0.1:${AEGYS_PORT}/healthz"
echo ""
echo "==> Deploy completed safely!"
