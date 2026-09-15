# Blindagem Web HTTP — Artigos Ana (2026-08-29)

Status: implementado e testado.

## O que foi blindado

### 1. Bind e timeouts (`backend/main.go:18`)
- Flag `-bind` padrão `127.0.0.1` (apenas local). Para expor na rede use `-bind=0.0.0.0` ou `BIND_ADDR=0.0.0.0` + `ALLOWED_ORIGIN`.
- `http.Server` com `ReadTimeout 15s`, `ReadHeaderTimeout 10s`, `WriteTimeout 30s`, `IdleTimeout 60s`, `MaxHeaderBytes 1MB` — mitiga Slowloris e header flood.
- Alerta quando `0.0.0.0` sem `JWT_SECRET` 32+ bytes ou sem `PGPASSWORD` forte.

### 2. CORS (`backend/internal/app/helpers.go:69`)
- Modo **estrito** quando `ALLOWED_ORIGIN` ou `GROK_ORIGIN` ou `STRICT_CORS=1` estiver setado.
- Estrito: só libera origens listadas explicitamente + `127.0.0.1:5173`/`localhost:5173`. Bloqueia `null`, `file://` e vazio, e `OPTIONS` de origem não listada retorna `403`.
- Não-estrito (desktop Electron): mantém `null`/`file://`/vazio para `file://` do Electron funcionar, como antes.
- `Vary: Origin`, `Allow-Credentials` só quando origin específico autorizado (nunca com `*`).

### 3. Security headers (`helpers.go:138`)
- `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`
- `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'`
- `Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=()`
- `X-Permitted-Cross-Domain-Policies: none`, `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy: same-origin`, `X-DNS-Prefetch-Control: off`
- `Strict-Transport-Security: max-age=31536000; includeSubDomains; preload` só com `https` (`TLS` ou `X-Forwarded-Proto: https`)
- `Cache-Control: no-store` para todas as rotas `/api/`

### 4. Auth (`handlers_auth.go:30` / `handlers_auth.go:372`)
- **Fix crítico:** removido bypass `strings.Contains(path, "/paginas/")` — `/api/artigos/{id}/paginas/{numero}/imagem|/camada` agora exigem `401` sem JWT (antes eram públicos).
- `/api/info` agora é público só em modo não-estrito (desktop); em modo estrito exige auth para não vazar IP LAN.
- Cookie `ana_session`: `HttpOnly` sempre, `Secure` quando `https`, `SameSite=Strict` (web). `SameSite=None` só em desktop não-estrito com `file://`/`null` (compatibilidade Electron) e nunca sem `Secure` em estrito.
- JWT HS256 com `hmac.Equal` e verificação de `alg` e `exp` (`verifyJWT` já ok).

### 5. Upload e body limits (`handlers_artigos.go:19` / `middleware.go:18`)
- Upload multipart em streaming com teto de PDF de `130MB` (mais envelope multipart controlado), assinatura `%PDF-` obrigatória, filename opcional e fallback de título seguro. O `Content-Type` é apenas informativo; a assinatura decide se o arquivo é PDF.
- Middleware `bodyLimitMiddleware` impõe `1MB` para JSON (`POST/PATCH/PUT`) e `132MB` para multipart (PDF de 130 MB mais envelope). Resposta `413` quando excede.
- Validação de título: máx `300` runes, sanitiza `\n`/`\r`.
- Handlers JSON tratam `request body too large` → `413` (antes virava `400`).

### 6. Rate limit (`middleware.go:11`)
- Global `120 req/min` por IP para todas as rotas `/api/` (via `generalRateLimitMiddleware` usando `sync.Map` com janela de 1 min). Excedeu → `429 Too Many Requests` + `Retry-After: 60`.
- Login mantém `5 falhas/60s → 15min 423` (já existente) com persistência em `login_tentativas`.
- `OPTIONS` não conta para rate limit.

### 7. Outros
- Imagem PNG: `Cache-Control: private, max-age=86400, immutable` (antes `public`) — evita cache compartilhado de conteúdo autenticado.
- SPA: checagem `strings.HasPrefix(alvo, WWWDir)` para travar path traversal e `Cache-Control` adequado para `/assets/`.
- `recoveryMiddleware` recupera `panic` sem vazar stack para cliente.
- SSRF em `handleEnriquecerCitacao` já tinha `isPrivateIP`/`isBlockedHost` + redirect limitado a 1 e `Timeout 8s` + limite `512KB` da resposta DuckDuckGo — mantido.
- SQL: todas as queries via `$1` placeholder (pgx) — sem injeção.

## Como rodar

Desktop (local apenas):
```
go run . -bind=127.0.0.1
```

Web na rede (hardened):
```
$env:JWT_SECRET="32+ bytes aleatórios forte"
$env:PGPASSWORD="senha forte"
$env:ALLOWED_ORIGIN="https://seu-front.web.app"
$env:BIND_ADDR="0.0.0.0"
go run . -bind=0.0.0.0
# ou com GROK:
$env:GROK_ORIGIN="https://abc.grok.io"
```

## Testes automatizados

Arquivo: `backend/internal/app/security_hardening_test.go` (11 testes):

- `TestHardening_Paginas_ExigeAuth` — 401 sem cookie, 200 com
- `TestHardening_Info_Estrito_ExigeAuth` — 401 em estrito sem auth, 200 com, 200 sem auth em não-estrito
- `TestHardening_CORS_Estrito` — evil/null/file bloqueados, allowed liberado, OPTIONS evil 403
- `TestHardening_CORS_NaoEstrito_PermiteFileNull`
- `TestHardening_SecurityHeaders_Reforcados` — Permissions-Policy etc + no-store em /api
- `TestHardening_BodyLimit_JSON_413` — 1.5MB JSON → 413
- `TestHardening_Upload_Validacoes` — .exe → 400, título 301 chars → 400
- `TestHardening_RateLimit_429` — 125 req → 429
- `TestHardening_Imagem_CachePrivate`
- `TestHardening_PathTraversal_WWW`
- `TestHardening_Cookie_Atributos`

Rodar:
```
cd backend
go test ./internal/app -run TestHardening -v
go test ./...  # todos (15s)
go vet ./...
```

Resultado atual: `ok` (todos passando).

## Checklist antes de expor na internet

- [ ] `JWT_SECRET` 32+ bytes gerado (`openssl rand -hex 32`)
- [ ] `PGPASSWORD` forte e não-padrão
- [ ] `ALLOWED_ORIGIN` com lista fechada (sem `*`)
- [ ] TLS via proxy/GROK (`X-Forwarded-Proto: https`) para HSTS e Secure cookie
- [ ] Firewall liberando só 8734 para IPs confiáveis se não for via GROK
- [ ] `BIND_ADDR=0.0.0.0` só quando intencional; desktop mantém `127.0.0.1`
- [ ] Rodar `go test ./internal/app -run TestHardening` e `go vet` antes do deploy

## Pendências (v2 se precisar)

- Senha real no login (hoje é só seleção de usuário) — para web pública, considerar adicionar `senha` bcrypt e `POST /auth/login {nome, senha}` obrigatório quando `STRICT_CORS=1`.
- WAF externo (fail2ban / rate limit no proxy) para complementar o rate limit in-memory.
- Log centralizado e alerta de 429/423.

