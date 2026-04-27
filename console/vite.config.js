import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// El frontend del Companion habla con dos servicios:
//   /v1/*       -> Nexus governance (proyecto separado, externo)
//   /companion  -> Companion backend (este mismo proyecto)
const governanceTarget = process.env.GOVERNANCE_PROXY_TARGET || 'http://host.docker.internal:18084'
const companionTarget = process.env.COMPANION_PROXY_TARGET || 'http://companion:8080'
const governanceAPIKey =
  process.env.GOVERNANCE_PROXY_API_KEY ||
  process.env.GOVERNANCE_API_KEY ||
  'governance-admin-dev-key'
const companionAPIKey =
  process.env.COMPANION_PROXY_API_KEY ||
  process.env.COMPANION_ADMIN_API_KEY ||
  'companion-admin-dev-key'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/v1': {
        target: governanceTarget,
        changeOrigin: true,
        headers: {
          'X-API-Key': governanceAPIKey,
        },
      },
      '/companion': {
        target: companionTarget,
        changeOrigin: true,
        headers: {
          'X-API-Key': companionAPIKey,
        },
        rewrite: (p) => p.replace(/^\/companion/, '') || '/',
      },
    },
  },
})
