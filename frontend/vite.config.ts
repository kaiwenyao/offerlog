import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

/**
 * Dev proxy: browser requests keep Origin: http://localhost:5173 while the
 * proxy rewrites Host to the API target. The backend's CSRF Origin check would
 * then reject every write (403). Two complementary fixes:
 *   - here, rewrite the forwarded Origin to the target's origin, and
 *   - backend DEV_ALLOWED_ORIGINS env var, for flows that skip this proxy
 *     (e.g. an external API host). Neither weakens production: the built SPA
 *     is same-origin and this configure only applies to the dev server.
 */
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        configure(proxy) {
          proxy.on('proxyReq', (proxyReq) => {
            proxyReq.setHeader('Origin', 'http://localhost:8080')
          })
        },
      },
      '/health': { target: 'http://localhost:8080' },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
    chunkSizeWarningLimit: 800,
  },
})
