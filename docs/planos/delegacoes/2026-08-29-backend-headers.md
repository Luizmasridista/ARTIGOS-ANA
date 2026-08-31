# Delegação — backend — headers PWA + cache (2026-08-29)

CONTEXT SWITCH -> backend

ROLE:
Act as senior backend Go 1.26 de Artigos Ana, especialista net/http stdlib + pgx + security headers. Priorize boring tech, YAGNI, single responsibility (um arquivo = um domínio), OWASP headers, e cache correto para PWA precache vs API no-store. Não quebrar CORS strict nem Electron file://.

TASK:
Garanta headers PWA no backend Go. Entregáveis: `backend/internal/app/helpers.go` (securityHeaders e Cache-Control) e `backend/internal/app/routes.go` (rota manifest se servido via Go, cache headers para /assets vs /api) garantindo `GET /api/health` e `GET /manifest.json` com `Cache-Control: public, max-age=3600` (ou immutable para /assets) e mantendo `no-store` para `/api/artigos` mutável. Escreva teste `pwa_headers_test.go`. NÃO commitar.

CONTEXT:
- Plano: `docs/planos/plano-2026-08-29-pwa-ipad.md:52` (§6 Backend: garantir GET /api/health e GET /manifest.json com public max-age 3600, manter no-store para /api/artigos já feito)
- ADR: `docs/adr/ADR-005.md` (techlead) + contrato PWA definirá manifest shape e sw.js rotas — leia antes; se ainda não existe, use plano §4/§8 como fallback
- Código atual: `backend/internal/app/helpers.go:181` (`securityHeaders` com X-Frame-Options SAMEORIGIN, X-Content-Type-Options nosniff, Referrer no-referrer, CSP default-src self, Permissions-Policy, X-Permitted, COOP same-origin, CORP same-origin, X-DNS-Prefetch off, Cache-Control no-store para /api/* linha 193, HSTS se TLS ou X-Forwarded-Proto https linha 198, Vary Origin); `backend/internal/app/routes.go:10` (Routes, /health e /api/health públicos linha 12-13, authMiddleware, ipadDetector, rateLimit, bodyLimit, withCORS, securityHeaders, recovery; se WWWDir != "" serve dist via fileServer + spa fallback linha 53, cache `public max-age=86400 immutable` para /assets/ linha 69, fallback index.html no-store linha 74); `backend/internal/app/app.go:15` (App com WWWDir, DataDir, JWTSecret); `backend/internal/app/middleware.go` (generalRateLimit, bodyLimit); `backend/internal/app/handlers_info.go` (handleInfo), `helpers.go:69` (withCORS strict + trycloudflare wildcard)
- Frontend PWA: `app/dist` servido via `-www` (backend -www flag serve SPA), Workbox precache precisa `Cache-Control: public max-age=3600` para manifest/sw/assets para permitir SW update, mas `no-store` para /api/artigos deve ser respeitado pelo SW (StaleWhileRevalidate vs NetworkOnly)
- Teste referência: `backend/internal/app/security_hardening_test.go` (headers), `backend/internal/app/app_test.go` — siga padrão
- Constraints: single writer: você é único escritor de `helpers.go` + `routes.go` neste ciclo; frontend é escritor de `app/vite.config.mts` etc — não toque lá; manter CORS trycloudflare + Electron file:// null

REASONING:
- Leia helpers.go:181 e routes.go:53 completos antes de editar; verifique single responsibility: helpers.go só helpers/security, routes.go só roteamento/cache — se acumular responsabilidade, quebre antes
- Analise Cache-Control: hoje `helpers.go:193` aplica `no-store` para TODOS /api/* (correto para /api/artigos mutável), `routes.go:69` aplica `public immutable` para /assets/ (correto precache), mas `/manifest.json`, `/sw.js`, `/health` precisam `public max-age=3600` (ou 86400) para SW precache sem staleness excessivo; decida com ADR: manifest não é /api/, então securityHeaders atual não o marca no-store — confirme e se necessário adicione branch `if r.URL.Path == "/manifest.json" || r.URL.Path == "/sw.js"` com public max-age
- Garanta que `/api/health` e `/health` continuam públicos e com `no-store`? Plano pede `public max-age=3600` para health para SW precache, mas helpers atual marca /api/health como no-store por prefix /api/. Avalie trade-off: health é idempotente, pode ser public 60s, mas /api/artigos deve permanecer no-store; proponha exceção: `if r.URL.Path == "/api/health"` → public 60s, demais /api/* → no-store; ou mantenha no-store e deixe SW fazer StaleWhileRevalidate respeitando no-store via `cacheWillUpdate` — justifique no código
- Não quebrar Electron file://: withCORS já permite null/file:// mesmo em strict (linha 122), manter
- HSTS: já condicional TLS (linha 198), manter; CSP já com `frame-ancestors 'self'` (linha 186) — standalone PWA não precisa frame
- Escreva teste `pwa_headers_test.go` RED→GREEN: `go test -run TestPwaHeaders` falhando antes, passando depois; cubra `GET /manifest.json` public, `GET /api/health` public/no-store conforme decisão, `GET /api/artigos` no-store, `GET /assets/index-abc.js` immutable
- Performance: p95 <100ms, sem alloc extra; headers estáticos
- YAGNI: não adicionar Redis/CDN logic, só headers

STOP CONDITIONS:
- [ ] `backend/internal/app/helpers.go` ajustado: `securityHeaders` define `Cache-Control` correto para ` /manifest.json` e `/sw.js` (`public, max-age=3600` ou 86400) e mantém `no-store` para `/api/artigos`, `/api/artigos/*`, POST/PATCH/DELETE; sem quebrar `no-store` existente para /api mutável; HSTS/CSP/X-Frame etc preservados
- [ ] `backend/internal/app/routes.go` garante que `WWWDir` serve `/manifest.json`, `/sw.js`, `/assets/*` com `public max-age` (se manifest servido via Go, não via Pages), fallback `index.html` com `no-store` preservado linha 74
- [ ] `GET /api/health` e `GET /health` respondem 200 com Cache-Control conforme decisão documentada (public 60s ou no-store com justificativa no ADR); `GET /manifest.json` 200 com public max-age 3600 e Content-Type application/json
- [ ] `go vet ./...` PASS, `go test ./... -run TestPwaHeaders -v` PASS com RED→GREEN evidência
- [ ] `withCORS` preservado: trycloudflare wildcard, Electron null/file://, strict mode, sem regressão
- [ ] DO NOT need: criar manifest.json no backend se frontend já gera em app/dist (apenas garantir headers quando servido via -www), não implementar SW logic, não mexer em app/vite.config.mts
- [ ] MUST NOT: commitar; SEM co-author; tocar em frontend files (single writer violation); quebrar auth middleware

OUTPUT:
- Arquivos entregues na working tree: `backend/internal/app/helpers.go`, `backend/internal/app/routes.go` (e se criar, `backend/internal/app/pwa_headers_test.go`)
- Teste `backend/internal/app/pwa_headers_test.go` com casos: manifest public, sw.js public, /assets immutable, /api/artigos no-store, /api/health decisão; evidência RED (fail antes) e GREEN (pass depois) colada no report
- Prova: `go vet ./...` ok, `go test ./... -v` 126+ PASS sem regressão, `curl -i http://127.0.0.1:8734/manifest.json` e `curl -i http://127.0.0.1:8734/api/health` mostrando Cache-Control
- Report curto PT-BR: headers garantidos, como testar (`go test -run TestPwaHeaders`, `curl -i`), working tree sem commit
- Entrega SEM commit, SEM co-author, SEM trailer; single writer respeitado

INSTRUÇÕES INVIOLÁVEIS:
NÃO commitar; deixar na working tree; git-ops versiona depois. SEM co-author, SEM trailer de agente, SEM atribuição de IA. Você é único escritor de `helpers.go`/`routes.go` neste ciclo; não edite `app/vite.config.mts` nem `app/src/main.tsx`.
