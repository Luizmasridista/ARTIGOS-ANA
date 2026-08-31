# Delegação — qa — Lighthouse PWA + offline (2026-08-29)

CONTEXT SWITCH -> qa

ROLE:
Act as senior QA de Artigos Ana, especialista Go + React/Electron + PostgreSQL, dono dos 4 Protocolos (E2E risco, isolamento anti-false, TDD RED→GREEN, SAST). Priorize jornadas usuário (BDD Given/When/Then), security sanity e edge cases, sem mockar implementação interna.

TASK:
Valide PWA iPad entregando QA gate. Entregáveis: testes `backend/internal/app/pwa_headers_test.go` (se backend não criou, crie/complemente), `app/src/registerSW.test.ts` (vitest), `app/e2e/pwa-lighthouse.mjs` + `docs/bdd/pwa.feature` cobrindo offline/standalone, e report Lighthouse PWA ≥90 em `vite preview`. NÃO commitar; TDD obrigatório.

CONTEXT:
- Plano: `docs/planos/plano-2026-08-29-pwa-ipad.md:56` (§6 QA: npm run build + vite preview --host + Lighthouse PWA 90+ iPad Mini simulation; teste iPad real Add to Home Screen standalone offline/online sync; §8 aceitação: manifest válido start_url "/" display standalone icons 192/512 theme #f7f7f5, sw.js precache dist, runtime GET /api/artigos* StaleWhileRevalidate, build+preview ok Lighthouse ≥90, sem regressão npm test 126 PASS go vet PASS)
- ADR: `docs/adr/ADR-005.md` (techlead) + contrato PWA — leia para saber manifest shape, sw.js rotas, registerSW API; se ausente use plano §4 fallback
- Entregas a validar: frontend `app/vite.config.mts` (VitePWA autoUpdate workbox runtimeCaching), `app/src/registerSW.ts` (só web !window.artigosAna), `app/public/manifest.json` + icons, `app/index.html` links, backend `helpers.go` Cache-Control public para manifest vs no-store para /api/artigos (ver `backend/internal/app/helpers.go:181`, `routes.go:69`)
- Stack teste: `app/package.json:14` (vitest 4.1.11, vite 8.2.2), `backend/go.mod` (go 1.26), `app/src/styles/README.md` (CSS modular), helpers CORS trycloudflare
- Protocolos QA vigentes: ver `.opencode/agents/qa.md` — must cobrir happy + vazio/corrompido + invasivo (XSS/SQL/IDOR/SSRF) + limite + race/timeout, SAST go vet + tsc + build

REASONING:
- Leia código real antes: `app/vite.config.mts`, `app/src/registerSW.ts`, `app/src/main.tsx`, `app/index.html`, `app/public/manifest.json`, `backend/internal/app/helpers.go`, `routes.go`, `docs/adr/ADR-005.md`; siga 4 protocolos sem exceção
- Desenhe cenários BDD primeiro em `docs/bdd/pwa.feature`: Given iPad Safari abre https://artigos-ana.web.app, When Add to Home Screen, Then standalone sem barra, Given offline após abrir artigo, When recarrega, Then artigo já aberto funciona (precache), Given online POST /api/artigos, When offline tenta POST, Then NetworkOnly falha graciosamente (sem cache), Given manifest fetch, Then start_url "/" display standalone
- TDD RED→GREEN: escreva `pwa_headers_test.go` e `registerSW.test.ts` antes, rode e CONFIRME falha esperada (FeatureNotImplemented ou 404), depois GREEN mínimo; se passar de primeira, teste viciado → reescrever; cole RED e GREEN logs no report
- Isolamento: testar comportamento observável, não mockar internos; mocks só para I/O externo (fetch, serviceWorker, CacheStorage); matriz limite obrigatória: manifest vazio/corrompido, icons ausentes, sw.js 404, /api/artigos 0/1/100 itens, duplicatas, MAX_INT, UTF-8, race 5× registerSW paralelo
- Security sanity: fuzz manifest.json com XSS `<script>alert(1)</script>` não deve executar, SSRF `http://127.0.0.1` rejeitado em runtimeCaching, IDOR trocar id artigo inexistente
- Lighthouse: rode `npm --prefix app run build` + `npx --prefix app vite preview --host --port 4173` + `npx lighthouse http://127.0.0.1:4173 --only-categories=pwa --chrome-flags="--headless"` ou Chrome DevTools manual; exigir PWA ≥90, manifest válido, sw registrado; simular iPad Mini viewport 1024×768
- SAST: `go vet ./...`, `npx --prefix app tsc --noEmit`, `npm --prefix app run build`, grep console.log/TODO/secrets, garantir sem payload XSS persistido
- Single responsibility: você é escritor de `*test.go`, `*.test.ts`, `e2e/*.mjs`, `docs/bdd/*.feature`; não edite `app/vite.config.mts` nem `helpers.go` (apenas teste)

STOP CONDITIONS:
- [ ] `docs/bdd/pwa.feature` existe com ≥5 cenários Given/When/Then cobrindo instalabilidade, standalone, offline precache, runtime GET /api/artigos* StaleWhileRevalidate, NetworkOnly POST/PATCH/DELETE
- [ ] `backend/internal/app/pwa_headers_test.go` cobre: GET /manifest.json → 200 public max-age 3600, GET /sw.js → public, GET /assets/* → immutable, GET /api/artigos → no-store, GET /api/health decisão conforme ADR, com RED→GREEN evidência
- [ ] `app/src/registerSW.test.ts` (vitest) cobre: não registra quando `window.artigosAna` existe (Electron), registra quando web + serviceWorker disponível, não quebra sem serviceWorker, com matriz limite (null/undefined, race)
- [ ] `app/e2e/pwa-lighthouse.mjs` automatiza `npm run build` + `vite preview` + fetch manifest.json válido (start_url "/" display standalone theme #f7f7f5 icons 192/512) + fetch sw.js 200 + check Cache-Control headers via Go se -www
- [ ] Lighthouse PWA ≥90 evidenciado: log `npx lighthouse --only-categories=pwa` ou screenshot DevTools Application→Manifest+Service Workers; se <90, report com gap e não declarar done
- [ ] SAST PASS: `go vet ./...` PASS, `npx tsc --noEmit` PASS, `npm run build` PASS, `npm test` 126+ PASS sem regressão, grep sem console.log/TODO/secrets
- [ ] DO NOT need: implementar PWA code (frontend/backend fazem), só validar; não commitar
- [ ] MUST NOT: declarar done sem evidência RED/GREEN + SAST + Lighthouse; mockar internos; commitar; SEM co-author violation

OUTPUT:
- Arquivos na working tree: `docs/bdd/pwa.feature`, `backend/internal/app/pwa_headers_test.go` (ou complemento), `app/src/registerSW.test.ts`, `app/e2e/pwa-lighthouse.mjs` (+ `app/e2e/pwa-offline.mjs` se criar)
- Evidência colada no report: RED fail log + GREEN pass log, `go vet` ok, `npx tsc --noEmit` ok, `npm run build` precache log, `curl -i` headers, `lighthouse --only-categories=pwa` score ≥90 ou gap listado
- Report curto PT-BR: o que foi testado (happy/vazio/invasivo/limite/race), o que passou, o que falta (iPad real manual), como reproduzir local (`go test -run TestPwaHeaders -v`, `npm test`, `npm run build && npx vite preview --host --port 4173` + Lighthouse)
- Entrega na working tree, SEM commit, SEM co-author, SEM trailer; QA gate aprovado/reprovado com critério mensurável

INSTRUÇÕES INVIOLÁVEIS:
NÃO commitar; deixar na working tree; git-ops versiona depois. SEM co-author, SEM trailer de agente, SEM atribuição de IA. Siga TDD RED→GREEN obrigatório; nunca escrever produção sem teste falhando antes. Você é único escritor dos arquivos de teste/bdd/e2e listados.
