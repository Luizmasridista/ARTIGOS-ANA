import { defineConfig } from 'vitest/config'
import type { Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'

function cspPlugin(): Plugin {
  return {
    name: 'html-csp',
    apply: 'build',
    transformIndexHtml(html) {
      const csp = [
        "default-src 'self'",
        "script-src 'self'",
        "style-src 'self' 'unsafe-inline'",
        "img-src 'self' data: blob: http://127.0.0.1:8734 http://127.0.0.1:8735 http://127.0.0.1:8736",
        "connect-src 'self' http://127.0.0.1:8734 http://127.0.0.1:8735 http://127.0.0.1:8736",
        "font-src 'self' data:",
        "object-src 'none'",
        "base-uri 'self'",
      ].join('; ')
      return html.replace(
        '<head>',
        `<head>\n    <meta http-equiv="Content-Security-Policy" content="${csp}">`,
      )
    },
  }
}

export default defineConfig({
  base: './',
  plugins: [
    react(),
    cspPlugin(),
    VitePWA({
      registerType: 'autoUpdate',
      injectRegister: null,
      includeAssets: ['favicon.ico', 'icons/*.png'],
      manifest: {
        name: 'Artigos Ana',
        short_name: 'Artigos Ana',
        description: 'Leitor de artigos PDF com marcacao de texto, notas e exportacao para DOCX',
        start_url: '/',
        scope: '/',
        display: 'standalone',
        background_color: '#f7f7f5',
        theme_color: '#f7f7f5',
        icons: [
          {
            src: '/icons/icon-192.png',
            sizes: '192x192',
            type: 'image/png',
            purpose: 'any maskable',
          },
          {
            src: '/icons/icon-512.png',
            sizes: '512x512',
            type: 'image/png',
            purpose: 'any maskable',
          },
        ],
      },
      workbox: {
        globPatterns: ['**/*.{js,css,html,ico,png,svg,woff2}'],
        runtimeCaching: [
          {
            urlPattern: /\/api\/artigos\/.*\/paginas\/.*\/camada/,
            handler: 'StaleWhileRevalidate',
            method: 'GET',
            options: {
              cacheName: 'api-camada-cache',
              expiration: { maxEntries: 50, maxAgeSeconds: 60 * 60 * 24 * 7 },
              cacheableResponse: { statuses: [0, 200] },
            },
          },
          {
            urlPattern: /\/api\/artigos/,
            handler: 'StaleWhileRevalidate',
            method: 'GET',
            options: {
              cacheName: 'api-artigos-cache',
              expiration: { maxEntries: 50, maxAgeSeconds: 60 * 60 * 24 * 7 },
              cacheableResponse: { statuses: [0, 200] },
            },
          },
          {
            urlPattern: /\/api\/.*/,
            handler: 'NetworkOnly',
            method: 'POST',
          },
          {
            urlPattern: /\/api\/.*/,
            handler: 'NetworkOnly',
            method: 'PATCH',
          },
          {
            urlPattern: /\/api\/.*/,
            handler: 'NetworkOnly',
            method: 'DELETE',
          },
        ],
      },
    }),
  ],
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
  },
  test: {
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    exclude: ['node_modules', 'dist', 'dist-electron', 'e2e', 'tests', 'playwright'],
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'esnext',
    cssCodeSplit: true,
    chunkSizeWarningLimit: 300,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules')) {
            if (id.includes('react')) return 'react-vendor'
          }
          if (id.includes('src/components/GrokBanner')) return 'grok'
          if (id.includes('src/components/PainelLateral') || id.includes('src/components/CitacoesTab')) return 'painel'
          if (id.includes('src/components/Biblioteca')) return 'biblioteca'
          if (id.includes('src/components/IpadLivePreview') || id.includes('src/styles/ipad-preview')) return 'ipad-preview'
        },
      },
    },
  },
})
