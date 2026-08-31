# Delegação — frontend — PWA iPad instalável (2026-08-29)

CONTEXT SWITCH -> frontend

ROLE:
Act as senior frontend de Artigos Ana, especialista React 19 + Vite 8 + TypeScript strict + CSS modular. Priorize single responsibility, hierarquia profissional (tokens.css), iPad HIG (44px, safe-area, dvh) e performance. Siga `app/src/styles/README.md` e `app/src/styles.css` (só @import).

TASK:
Implemente PWA instalável iPad no frontend. Entregáveis na working tree: `app/public/manifest.json` + ícones `app/public/icons/icon-192.png` + `icon-512.png` + `apple-touch-icon.png`, `app/vite.config.mts` com `VitePWA`, `app/src/registerSW.ts`, `app/src/main.tsx` com registro condicional, `app/index.html` com links manifest/icons, e update `app/src/styles/README.md` se criar CSS novo. NÃO commitar.

CONTEXT:
- ADR bloqueante: `docs/adr/ADR-005.md` (a ser escrito pelo techlead) + contrato PWA — AGUARDE ADR antes de codar; leia-o primeiro; se ADR ainda não existe, leia `docs/planos/plano-2026-08-29-pwa-ipad.md:30` (§4 arquitetura: VitePWA Workbox precache dist/assets, runtime StaleWhileRevalidate GET /api/artigos* max 50/7d, NetworkOnly POST/PATCH/DELETE, registerType autoUpdate, manifest start_url "/" display standalone theme #f7f7f5)
- Arquivos atuais: `app/vite.config.mts:28` (defineConfig base './' com react()+cspPlugin, sem VitePWA), `app/index.html:6` (já tem viewport-fit, apple-mobile-web-app-capable yes, theme #f7f7f5, favicon /favicon.ico), `app/src/main.tsx:1` (createRoot App, iPad detection data-device="ipad" via isIpadUA, sem SW; isPreviewRoute + isIpadFrame), `app/src/styles/README.md:1` (18 arquivos modulares, tokens→base→...→grok), `app/src/styles.css:1` (só @imports), `app/package.json:25` (deps react 19.2.8, dev vite 8.2.2, @vitejs/plugin-react 6.1.0, sem vite-plugin-pwa), `app/src/hooks/useIpad.ts:1` (isIpadUA, detectIpad), `backend/internal/app/helpers.go:193` (Cache-Control no-store para /api/, public immutable para /assets/ em routes.go:69)
- Constraints: Workbox precache `dist/assets` + `index.html`, runtime `GET /api/artigos*` StaleWhileRevalidate, `GET /api/artigos/*/paginas/*/camada` idem, `NetworkOnly` para POST/PATCH/DELETE, SW só em web `if (!window.artigosAna)`, manifest icons 192/512, display standalone, theme #f7f7f5, iOS Cache Storage ~50MB → expiration maxEntries 50 maxAge 7d
- Design: sem CSS monolito; se precisar estilo para update prompt/offline fallback, crie `app/src/styles/pwa.css` e importe em `styles.css` (single responsibility)

REASONING:
- Leia vite.config.mts, index.html, main.tsx, styles/README.md, ADR-005 antes de codar; verifique single responsibility antes: um arquivo = um domínio; nunca misture biblioteca com leitor ou append 300 linhas no monolito
- Siga ADR-005 exatamente: se ADR definir vite-plugin-pwa Workbox com runtimeCaching específico, replique sem inventar; se ADR não existir ainda, use plano §4 como fallback mas marque TODO para alinhar pós-ADR
- Instale `vite-plugin-pwa` compatível Vite 8 (verificar `npm view vite-plugin-pwa version` ou usar ^0.21+), configure `VitePWA({ registerType: 'autoUpdate', includeAssets: ['favicon.ico','icons/*.png'], manifest: { name "Artigos Ana", short_name "Artigos Ana", start_url "/", display "standalone", background_color "#f7f7f5", theme_color "#f7f7f5", icons [...] }, workbox: { globPatterns: ['**/*.{js,css,html,ico,png,svg,woff2}'], runtimeCaching: [...] } })`; respeite `base: './'` vs PWA precisa `base: '/'` para start_url "/" — decida com ADR
- Crie `app/src/registerSW.ts` com `export function registerSW(): void` que checa `!window.artigosAna` e ` 'serviceWorker' in navigator`, registra `/sw.js` ou usa `virtual:pwa-register`, handler update (autoUpdate não precisa prompt, mas log). Importe em `main.tsx` após iPad detection, só em web
- Atualize `app/index.html`: adicione `<link rel="manifest" href="/manifest.json">` (ou /public/manifest.json conforme VitePWA gera), `<link rel="apple-touch-icon" href="/icons/apple-touch-icon.png">`, mantenha existentes viewport/theme
- Gere ícones 192/512: pode copiar/resize favicon.ico ou criar placeholder PNG; se não tiver imagem, crie via script canvas ou copie favicon como png temporário e documente TODO para designer
- CSP em vite.config.mts: atualizar connect-src se SW precisar fetch same-origin; manter style-src unsafe-inline para Vite dev
- SEM Electron: SW nunca em `window.artigosAna` (desktop), teste `if (!window.artigosAna)` antes de registrar

STOP CONDITIONS:
- [ ] `app/vite.config.mts` importa `VitePWA` e configura `registerType autoUpdate`, `includeAssets`, `manifest` com `start_url "/"`, `display "standalone"`, `theme_color "#f7f7f5"`, `icons 192/512`, `workbox.globPatterns` cobre assets e `workbox.runtimeCaching` com `urlPattern GET /api/artigos*` StaleWhileRevalidate (expiration 50/7d) + `GET /api/artigos/*/paginas/*/camada` idem + NetworkOnly para POST/PATCH/DELETE (method POST)
- [ ] `app/src/registerSW.ts` existe, exporta `registerSW`, registra SW só se `!window.artigosAna` e `navigator.serviceWorker`
- [ ] `app/src/main.tsx` chama `registerSW()` condicional web (após `document.documentElement.dataset.modo = 'web'`), sem quebrar Electron; mantém iPad detection existente
- [ ] `app/index.html` contém `<link rel="manifest"` e `<link rel="apple-touch-icon"` com hrefs corretos, mantém viewport-fit e theme-color
- [ ] `app/public/manifest.json` (ou `app/manifest.json` conforme ADR) válido JSON com `start_url "/"`, `display "standalone"`, `theme_color "#f7f7f5"`, `icons` 192 e 512; `app/public/icons/*` existem (192,512, apple-touch)
- [ ] `app/src/styles/README.md` atualizado se criar `pwa.css` (ou sem alteração se não precisar CSS); `app/src/styles.css` continua só @import, sem monolito
- [ ] `npm --prefix app run build` passa e gera `dist/manifest.webmanifest` ou `dist/manifest.json` + `dist/sw.js` + precache entries; `npx tsc --noEmit` sem erro
- [ ] DO NOT need: backend headers (outro agente), QA Lighthouse (outro agente) — mas não quebre backend
- [ ] MUST NOT: commitar; SEM co-author; single writer violado (não tocar em `backend/internal/app/helpers.go`)

OUTPUT:
- Arquivos entregues na working tree: `app/vite.config.mts`, `app/src/registerSW.ts`, `app/src/main.tsx`, `app/index.html`, `app/public/manifest.json`, `app/public/icons/*`, `app/src/styles/README.md` (+ `pwa.css` se criar)
- Prova: `npm --prefix app run build` log mostrando precache + sw.js gerado, `npx tsc --noEmit` ok, screenshot ou log de `dist/manifest.json` válido
- Teste: criar `app/src/registerSW.test.ts` ou `app/src/components/Pwa.test.tsx` cobrindo `!window.artigosAna` branch (vitest), opcional mas recomendado
- Report curto PT-BR: o que foi feito, como testar (`npm run build` + `vite preview --port 4173` + Application→Manifest), branch working tree, sem commit
- Entrega SEM commit, SEM co-author, SEM trailer; single writer respeitado; CSS modular preservado

INSTRUÇÕES INVIOLÁVEIS:
NÃO commitar; deixar na working tree; git-ops versiona depois. SEM co-author, SEM trailer de agente, SEM atribuição de IA. Você é único escritor dos arquivos frontend listados; não toque em backend. Verifique `app/src/styles/README.md` antes de editar CSS.
