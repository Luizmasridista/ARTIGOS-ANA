import { defineConfig } from 'vitest/config'
import type { Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'

function faviconFixPlugin(): Plugin {
  return {
    name: 'favicon-fix',
    // garante que favicon/icons sejam absolutos com ?v=2 mesmo com base './' (Electron)
    // Vite reescreve "/favicon.ico" -> "./favicon.ico" quando base='./', este hook corrige após.
    transformIndexHtml(html) {
      let out = html
      // corrige favicon.ico relativo -> absoluto versionado
      out = out.replaceAll('href="./favicon.ico', 'href="/favicon.ico')
      out = out.replaceAll('href="./icons/', 'href="/icons/')
      // garante ?v=2 cache-bust se ainda não tiver query
      out = out.replaceAll('/favicon.ico"', '/favicon.ico?v=2"')
      out = out.replaceAll('/favicon.ico?v=2?v=2"', '/favicon.ico?v=2"')
      // apple-touch já versionado no source, mas garante relativo também
      out = out.replaceAll('/apple-touch-icon.png"', '/apple-touch-icon.png?v=2"')
      out = out.replaceAll('/apple-touch-icon.png?v=2?v=2"', '/apple-touch-icon.png?v=2"')
      out = out.replaceAll('/favicon-32x32.png"', '/favicon-32x32.png?v=2"')
      out = out.replaceAll('/favicon-32x32.png?v=2?v=2"', '/favicon-32x32.png?v=2"')
      out = out.replaceAll('/favicon-16x16.png"', '/favicon-16x16.png?v=2"')
      out = out.replaceAll('/favicon-16x16.png?v=2?v=2"', '/favicon-16x16.png?v=2"')
      return out
    },
  }
}

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
    faviconFixPlugin(),
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
        navigateFallback: 'index.html',
        navigateFallbackDenylist: [
          /^\/api/,
          /^\/health/,
          /^\/favicon\.ico/,
          /^\/icons\//,
          /^\/manifest/,
          /^\/sw\.js/,
          /^\/workbox/,
        ],
        cleanupOutdatedCaches: true,
        runtimeCaching: [
          {
            urlPattern: /\/api\/auth\/.*/,
            handler: 'NetworkOnly',
            method: 'GET',
          },
          {
            urlPattern: /\/api\/auth\/.*/,
            handler: 'NetworkOnly',
            method: 'POST',
          },
          {
            urlPattern: /\/api\/health/,
            handler: 'NetworkOnly',
          },
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
