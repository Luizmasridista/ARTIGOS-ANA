# ADR-008 — Governança por IP/dispositivo (restrição a PCs/iPads autorizados)

Status: aceito | Data: 2026-08-31 | Autor: backend

## Contexto

Artigos Ana deve rodar apenas nos dispositivos autorizados: PC LENOVO `PE0***MM` (IP público atual `189.6.xxx.xxx`, locais `192.168.0.94/69`) e iPad `C97***66Y`, mesmo quando exposto na WEB via Render `https://artigos-ana.onrender.com` (Neon Postgres). IP é dinâmico, então allowlist precisa ser atualizável sem redeploy. iPad navegador não expõe serial via JS, precisa alternativa via token `X-Device-Id` gerado em `offline/sync.ts:getOrCreateDeviceId` + confirmação manual de serial. Render Free envia IP real via `X-Forwarded-For` (proxy), fallback `RemoteAddr`. Healthchecks `/health` e `/api/health` precisam continuar públicos.

## Decisão

### Tabela `dispositivos_autorizados` (store.go schemaStatements)
- `id BIGSERIAL PK`, `identificador TEXT NOT NULL`, `tipo TEXT NOT NULL`, `descricao TEXT`, `criado_em TIMESTAMPTZ DEFAULT now()`, `UNIQUE(identificador,tipo)`.
- Tipos: `ip`, `device_token`, `serial_hash` / `pc_serial_hash` / `ipad_serial_hash`.
- Serial nunca em claro: guarda `SHA256(serial + JWT_SECRET)` (hex). Salt é `JWT_SECRET` para não vazar serial em dump/log.
- Índices em `tipo` e `identificador`.

### Middleware `governancaMiddleware` (app/governanca.go)
- Bypass: `GET /health`, `GET /api/health` e `OPTIONS` (preflight) sempre passam.
- Enforce: `GOVERNANCE_ENFORCE=1|true` força; `=0|false` desliga; se vazio, auto `BIND_ADDR=0.0.0.0` => liga, caso contrário desliga (fallback dev não trava, testes httptest passam).
- Extração IP: `X-Forwarded-For` primeiro IP, fallback `r.RemoteAddr` normalizado (remove porta).
- Extração device: headers `X-Device-Id` / `X-Device-Token` (também `X-Device-Serial-Hash` para Electron).
- Allowlists:
  - Env `ALLOWED_IPS` (vírgula, suporta CIDR `192.168.0.0/24` e IP exato).
  - Env `ALLOWED_DEVICE_TOKENS` (ou `ALLOWED_DEVICE_IDS` compat).
  - Tabela `dispositivos_autorizados` (`ip`, `device_token`, `serial_hash`).
- Se IP **ou** device token **ou** serial hash autorizado => permite. Caso contrário `403 {"erro":"acesso restrito — dispositivo não autorizado"}` e `log.Printf("governanca bloqueado ip=%s ua=%s device=%s path=%s", maskIP(ip), ua, maskDevice(deviceId), path)`.
- Nunca loga serial/IP completo: `maskIP` mostra `189.6.xxx.xxx`, `maskDevice` mostra `ab***cd`.
- Ordem no `routes.go`: `auth -> governanca -> ipadDetector -> rateLimit -> bodyLimit -> cors -> security -> recovery`.
  - Health passa antes de governança; login também é governado (só IPs/devices autorizados podem autenticar, que é o requisito "só rode nos dois dispositivos").
  - Em dev `GOVERNANCE_ENFORCE=0` a cadeia permite tudo.

### Endpoints governança (auth protegido via `authMiddleware`)
- `POST /api/governanca/registrar` `{serial?, deviceId?, ip?, tipo?}`: registra `device_token` e/ou `ip` e/ou hash serial. Validação de tamanho mínimo, hash com `JWT_SECRET`. Usado no primeiro acesso: usuário digita serial do iPad/PC, frontend envia `deviceId` (getOrCreateDeviceId) + serial. Também serve para atualizar IP dinâmico sem redeploy.
- `GET /api/governanca/dispositivos`: lista autorizados (hashes, não serial em claro).
- `DELETE /api/governanca/dispositivos/{id}` e `GET /api/governanca/status` (diagnóstico mascarado).

### CORS (helpers.go)
- `Access-Control-Allow-Headers` expandido para `Content-Type, Authorization, X-Device-Id, X-Device-Token, X-Device-Serial-Hash, X-Device`.

### Frontend (app/src/api.ts, offline/session.ts)
- `getOrCreateDeviceId` agora persiste em `localStorage ana_device_id` além de IndexedDB.
- `montarHeadersAutenticados` injeta `X-Device-Id` síncrono a cada `fetch`.
- `X-Device: iPad` já existia para heurística iPadOS.

### Env
- `backend/.env.web.example`: `GOVERNANCE_ENFORCE=1`, `ALLOWED_IPS=189.6.213.149,192.168.0.94,192.168.0.69,127.0.0.1`, comentário sobre `ALLOWED_DEVICE_TOKENS` e registro via tabela.
- `render.yaml`: `GOVERNANCE_ENFORCE=1`, `ALLOWED_IPS sync:false`, `ALLOWED_DEVICE_TOKENS sync:false`.

## Consequências

- Produção só acessível de IPs/devices na allowlist (env ou tabela). Troca de IP dinâmico: atualizar `ALLOWED_IPS` no Render ou `POST /api/governanca/registrar {ip:"novo.ip"}` autenticado.
- Novo iPad/PC: fazer login a partir de IP já autorizado, chamar `POST /api/governanca/registrar {deviceId:"<uuid>", serial:"C97***66Y"}` (serial é hasheado server-side).
- Healthcheck Render continua `200` mesmo sem IP autorizado.
- Dev local e testes com `GOVERNANCE_ENFORCE=0` ou `BIND_ADDR!=0.0.0.0` não são afetados; `go vet`/`go test` passam.
- Serial nunca em claro no código, dump ou log; apenas hash SHA256+salt.

## Alternativas rejeitadas

- Bloquear via browser API de serial: inexistente na WEB.
- Allowlist apenas via env sem tabela: exigiria redeploy a cada troca de IP dinâmico.
- Middleware apenas pós-auth: deixaria `/api/auth/login` exposto a brute-force de IPs não autorizados.

## Validação

- `go vet ./...` ok
- `go test ./...` ok (skip DB quando sem Postgres)
- `curl /health 200` sem header autorizado mesmo com `GOVERNANCE_ENFORCE=1`
- `curl /api/artigos 403` sem IP/device autorizado em produção, `200` com `X-Forwarded-For: 189.6.213.149` ou `X-Device-Id` registrado
- `POST /api/governanca/registrar` exige auth (401 sem cookie) e grava hashes sem logar serial
