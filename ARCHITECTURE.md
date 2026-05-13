# Companion Architecture

Companion es el servicio central de IA del ecosistema: el empleado IA. Su
responsabilidad es conversar, razonar, recordar, planificar, usar tools y
ejecutar acciones asistidas. Nexus decide gobernanza; los productos exponen
capacidades de dominio.

## Módulos actuales

| Módulo | Responsabilidad |
|---|---|
| `cmd/api` | Bootstrap HTTP, config, migraciones, middleware y shutdown |
| `wire` | Composición de dependencias, auth, clients y loops |
| `internal/tasks` | Lifecycle de tasks, chat, propuestas a Nexus y ejecución |
| `internal/agents` | Perfiles seedables, autonomy y allowlists de tools |
| `internal/runtime` | LLM orchestration, prompt, tool calling, control plane y traces |
| `internal/connectors` | Registry de connectors, capabilities, idempotencia y evidence |
| `internal/memory` | Memoria por scope `task/org/user` con TTL y cuota |
| `internal/watchers` | Automatizaciones proactivas sobre capabilities de producto |
| `internal/governance_assist` | Helpers IA para explicar/proponer sobre Nexus |
| `console` | UI Companion |
| `migrations` | Esquema Postgres |

## Flujos

- Chat: `/v1/chat` crea o reutiliza task, persiste mensaje, llama al runtime,
  ejecuta tools permitidas y guarda respuesta/traces.
- Task governance: task -> propose -> Nexus `SubmitRequest` -> sync -> estado
  Companion.
- Execution: execution plan -> validación de governance -> connector capability
  -> evidence/result -> task verification.
- Memory: upsert/find/get/delete por scope; runtime solo recuerda si tiene
  identidad válida.
- Watchers: consultan capabilities read del producto, crean proposals,
  consultan Nexus y ejecutan side effects vía connectors.

## Persistencia

Postgres guarda tasks, messages, actions, artifacts, governance sync state,
execution plans/state, watchers/proposals, memory entries, connectors/executions
y run traces. `companion_run_traces` incluye `prompt_version` y `model` para
auditar runtime IA.

## Runtime IA

El runtime usa providers de `core/ai/go`. El prompt tiene versión
`companion.system.v1`. El control plane construye una `IdentityChain`, un
`AgentRoute` y un `AgentProfile` efectivo con allowlist de tools. El LLM solo
recibe schemas autorizados para tenant/scopes presentes.

## Configuración local

El servicio requiere `DATABASE_URL`, `COMPANION_API_KEYS`,
`GOVERNANCE_BASE_URL` y `GOVERNANCE_API_KEY`. Pymes, Ponti, OIDC y LLM real son
opcionales. Ver `.env.example` y `OPERATIONS.md`.
