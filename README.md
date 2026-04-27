# Companion

Empleado IA transversal del ecosistema. Consume **Nexus Governance** (proyecto
separado) para todas las acciones que requieran approval/audit.

> La DB se llama `nexus_companion` por consistencia histórica con el resto
> del ecosistema; el módulo Go es `github.com/devpablocristo/companion`.

## Estructura

```
companion/
├── cmd/api/                 # entry point del backend Go
├── internal/                # módulos: tasks, runtime, connectors, memory, watchers
├── wire/                    # DI manual + cliente HTTP a Nexus governance
├── migrations/              # PostgreSQL embebidas
├── console/                 # frontend (React + Vite + TS)
├── scripts/
│   ├── lib/common.sh
│   ├── smoke/run-companion-*.sh
│   ├── dev/ensure-companion-db.sh
│   └── quality/{check-migrations,go-in-env}.sh
├── Dockerfile
├── docker-compose.yml       # companion + companion-postgres + console
├── go.mod
├── Makefile
└── .env.example
```

## Requisitos

- PostgreSQL (la DB `nexus_companion` se crea automáticamente desde el container).
- **Nexus Governance** accesible vía `GOVERNANCE_BASE_URL` y `GOVERNANCE_API_KEY`
  (proyecto separado en `../nexus/`).

## Arranque rápido

Levantá Nexus governance primero (en `../nexus/`):

```bash
cd ../nexus
make up
```

Después companion (este repo):

```bash
test -f .env || cp .env.example .env
make up
```

URLs por defecto (host):

| Servicio       | URL                       |
|----------------|---------------------------|
| Companion API  | `http://localhost:18085`  |
| Companion UI   | `http://localhost:13002`  |
| Nexus Gov API  | `http://localhost:18084`  |

## Variables de entorno

Ver `.env.example`.

Convenciones:
- `GOVERNANCE_BASE_URL`, `GOVERNANCE_API_KEY` — apuntan al servicio Nexus governance externo.
- `COMPANION_API_KEYS` (dentro del container) — auth del propio companion.
- `COMPANION_AUTH_*` — OIDC/JWKS opcional para sesión humana.
- `COMPANION_LLM_PROVIDER` / `COMPANION_LLM_API_KEY` / `COMPANION_LLM_MODEL`
  — runtime IA del companion.
- `COMPANION_GOVERNANCE_SYNC_INTERVAL_SEC` — período del loop que reconcilia
  decisiones de governance con propuestas pendientes.

## Tests

```bash
make test                    # Go unit
make qa                      # build + vet + test -race
make smoke                   # smoke contra companion + nexus levantados
```

## Próximos pasos sugeridos

1. (opcional) Podar el `console/` de las vistas que NO consumen este repo:
   las de governance pura (`Policies`, `Audit`, `ActionTypes`, `Config`,
   `Dashboard`, `Learning`, `Requests`). Companion necesita: `Tasks`,
   `Connectors`, `Memory`, `Chat`, `Sandbox`, `Agents`, `Home`, y `Inbox`/`Replay`
   por el feed mixto que mezcla approvals de Nexus con tasks de Companion.
2. Configurar el deploy productivo: ajustar `console/nginx.conf.template`
   para apuntar a la URL real de Nexus governance (hoy default
   `http://host.docker.internal:18084`).
