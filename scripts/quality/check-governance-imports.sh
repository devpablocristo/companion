#!/usr/bin/env bash
# Bloquear cualquier import de los packages de governance que vivían en
# core/governance/go antes de v0.4.0 y que ahora solo existen en
# nexus/governance/internal/. Si alguien los vendoreara, copiara, o usara
# una versión vieja de core, este check rompe el build.
#
# La regla del ecosistema es absoluta: la lógica de governance vive en
# Nexus. Productos consumen via HTTP (governanceclient). Sin excepciones.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

# Patrones prohibidos: cualquier import del shared kernel viejo.
PROHIBITED='core/governance/go/(decision|policy|risk|approval|kernel)'

# Buscamos en archivos .go pero excluimos vendor/ y dirs de build.
matches="$(
  cd "$ROOT" && \
    git grep -nE "\"github\\.com/devpablocristo/${PROHIBITED}" -- '*.go' || true
)"

if [ -n "$matches" ]; then
  echo "ERROR: imports prohibidos de core/governance/go encontrados:" >&2
  echo "$matches" >&2
  echo >&2
  echo "Estos packages fueron eliminados en governance/go v0.4.0." >&2
  echo "La lógica de governance vive solo en nexus/governance/internal/." >&2
  echo "Productos deben llamar a Nexus via HTTP usando governanceclient." >&2
  exit 1
fi

echo "Governance imports check passed."
