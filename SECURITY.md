# Security

## Auth

Companion requiere API key u OIDC/JWT. El middleware inyecta:

- `X-User-ID`
- `X-Org-ID`
- `X-Auth-Role`
- `X-Auth-Scopes`
- `X-Auth-Method`
- `X-Service-Principal`

API keys soportan metadata inline: `actor`, `org_id`, `scopes` y
`service_principal`.

## Scopes

Endpoints sensibles usan scopes: tasks, connectors, watchers y
governance-assist. El API key admin de dev incluye todos los scopes necesarios.

## Multi-tenant

- Tasks listadas por tenant ya no incluyen tasks con `org_id` vacío.
- Un principal con `X-Org-ID` no puede acceder tasks con `org_id` vacío.
- Watcher alerts preservan `OrgID`.
- Memory valida scope contra usuario/org/task.
- Connector executions rechazan connectors globales con `org_id` vacío.
- Runtime tools requieren tenant/user/scopes antes de exponerse al LLM.

## Prompt injection

El runtime rechaza patrones básicos de prompt injection en mensajes y args de
tools. Esto es una guardrail mínima, no una política de seguridad completa.

## Secret handling

Evidence de connector executions sanitiza claves sensibles conocidas. No se
deben registrar API keys, bearer tokens ni payloads sensibles sin redacción.
