import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// El frontend del Companion habla solamente con Companion. Nexus governance
// vive en otro proyecto y Companion lo consume server-side.
const companionTarget = process.env.COMPANION_PROXY_TARGET || 'http://companion:8080'
const companionAPIKey =
  process.env.COMPANION_PROXY_API_KEY ||
  process.env.COMPANION_ADMIN_API_KEY ||
  'companion-admin-dev-key'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
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
