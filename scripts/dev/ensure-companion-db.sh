#!/usr/bin/env bash
# Crea la base nexus_companion si no existe (útil cuando el volumen de Postgres
# se creó antes de montar postgres-init).
set -euo pipefail

V3_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$V3_ROOT"

if ! docker compose exec -T governance-postgres pg_isready -U postgres >/dev/null 2>&1; then
  echo "ERROR: Postgres no responde. Desde v3/: docker compose up -d governance-postgres" >&2
  exit 1
fi

EXISTS=$(docker compose exec -T governance-postgres psql -U postgres -Atqc \
  "SELECT 1 FROM pg_database WHERE datname = 'nexus_companion'" || true)
if [ "$EXISTS" = "1" ]; then
  echo "OK: database nexus_companion already exists"
  exit 0
fi

echo "Creating database nexus_companion..."
docker compose exec -T governance-postgres psql -U postgres -c "CREATE DATABASE nexus_companion;"
echo "OK: nexus_companion created"
