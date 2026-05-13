# Operations

## Local

```bash
test -f .env || cp .env.example .env
make up
make logs
```

Nexus Governance debe estar levantado aparte y accesible por
`GOVERNANCE_BASE_URL`.

Los connectors de ejecución son tenant-owned. El registry publica templates de
capabilities, pero cada tenant debe crear su fila `/v1/connectors` con
`X-Org-ID`; no hay fallback operativo a `org_id=''`.

## Health

- `GET /healthz`: proceso vivo.
- `GET /readyz`: DB disponible.

## Migrations

El backend aplica migraciones embebidas al arrancar. Validar versiones con:

```bash
bash scripts/quality/check-migrations.sh
```

## Background loops

- `COMPANION_GOVERNANCE_SYNC_INTERVAL_SEC`: sync de tasks con Nexus.
- `COMPANION_STRICT_GOVERNANCE`: activa fail-closed estricto para grants Nexus.
- `COMPANION_WATCHER_INTERVAL_SEC`: ejecución periódica de watchers.
- `COMPANION_WATCHER_SYNC_INTERVAL_SEC`: reconciliación de watcher proposals.
- Memory purge corre cada hora.

## Smoke

```bash
make smoke
```

Los smoke scripts esperan Companion y Nexus levantados, y usan las keys de
`.env`.
