#!/usr/bin/env bash
# Start a local postgres for OfferLog dev (Docker).
set -euo pipefail
NAME=offerlog-pg-dev
if docker ps --format '{{.Names}}' | grep -q "^${NAME}$"; then
  echo "postgres ${NAME} already running"
  exit 0
fi
docker rm -f ${NAME} 2>/dev/null || true
docker run -d --name ${NAME} \
  -e POSTGRES_USER=offerlog -e POSTGRES_PASSWORD=offerlog \
  -e POSTGRES_DB=offerlog \
  -p 5433:5432 postgres:17-alpine
echo "waiting for postgres..."
for i in $(seq 1 30); do
  if docker exec ${NAME} pg_isready -U offerlog >/dev/null 2>&1; then
    echo "postgres ready on localhost:5433"
    exit 0
  fi
  sleep 1
done
echo "postgres did not become ready" >&2
exit 1
