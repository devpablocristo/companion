# Boundaries

## Regla principal

IA vive en Companion. Gobernanza vive en Nexus. Dominio vertical vive en los
productos.

## Companion

Companion puede:

- Orquestar LLMs, agents, tools y memoria.
- Decidir qué capability quiere invocar.
- Preparar evidence y contexto operativo.
- Consultar Nexus antes de acciones sensibles.
- Persistir traces operativas del runtime IA.

Companion no puede:

- Evaluar policies.
- Aprobar o rechazar requests como motor de governance.
- Reimplementar risk engine o audit fuerte.
- Guardar memoria sin tenant/user/product context cuando aplique.
- Mezclar datos entre tenants.

## Nexus

Nexus decide `allow`, `deny` o `require_approval`, administra approvals,
policies, risk y audit fuerte. Nexus no debe importar runtime LLM, memoria IA ni
agents.

## Productos

Pymes, Ponti u otros productos exponen capabilities y manifiestos. Su lógica
vertical no debe crecer dentro de Companion. `internal/watchers` debe operar
contra capabilities genéricas; el código vertical queda encapsulado en adapters
de connector o en el producto.

## Core y Modules

`core/*` son primitivas técnicas: HTTP clients, DB, auth, logger, errores,
middlewares y clientes Nexus. En este repo no hay carpeta local `core`; se
consume como dependency externa.

`modules/*` no existe actualmente en este repo. Si se agregan componentes UI
compartidos, deben ser reutilizables y sin dominio pesado.
