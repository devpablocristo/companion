import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// El frontend del Companion habla con dos servicios:
//   /v1/*       -> Nexus governance (proyecto separado, externo)
//   /companion  -> Companion backend (este mismo proyecto)
const nexusTarget = process.env.NEXUS_PROXY_TARGET || 'http://host.docker.internal:18084'
const companionTarget = process.env.COMPANION_PROXY_TARGET || 'http://companion:8080'
const nexusAPIKey =
  process.env.NEXUS_PROXY_API_KEY ||
  process.env.NEXUS_API_KEY ||
  'nexus-admin-dev-key'
const companionAPIKey =
  process.env.COMPANION_PROXY_API_KEY ||
  process.env.COMPANION_ADMIN_API_KEY ||
  'companion-admin-dev-key'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/v1': {
        target: nexusTarget,
        changeOrigin: true,
        headers: {
          'X-API-Key': nexusAPIKey,
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
