# Captura indireta via rede — blindagem mínima (2026-08-29)

Foco: atacante não ataca o server diretamente, mas fareja a rede (WiFi compartilhado, proxy, cache) para capturar dados.

## Vetores testados

| Vetor | Como ataca | O que captura | Resultado com blindagem atual |
|-------|------------|---------------|------------------------------|
| **Sniffing passivo http** | Wireshark/ARP spoof na LAN, proxy transparente | `Set-Cookie: ana_session=...`, `Authorization: Bearer ...`, PDFs, notas, `GET /api/artigos` | **Sem TLS**: tráfego em claro, cookie pode ser lido. **Mitigado** com `TLS_CERT/TLS_KEY` ou `GROK https` (ver abaixo). |
| **Replay** | Captura cookie e reenvia de IP diferente `CF-Connecting-IP: 9.9.9.99` | Sessão hijack | **Replay funciona** sobre http (sem vínculo IP) — demonstra que sniffing = hijack total. Protegido apenas com TLS para impedir captura. |
| **Cache proxy** | Proxy corporativo cacheia `GET /api/artigos` | Lista de artigos, notas | **Mitigado** `Cache-Control: no-store, no-cache, must-revalidate` + `Pragma: no-cache` em todas `/api/` (`helpers.go:182`) |
| **Downgrade MITM** | Atacante força http quando usuário digitou http | Remove https e fareja | **Mitigado** quando via `https`: `HSTS max-age=31536000; includeSubDomains; preload` (`helpers.go:187`) + `X-Forwarded-Proto: https` do GROK |
| **XSS → localStorage** | Injeta `<script>` via nota, lê `localStorage.getItem('ana_token')` | Token JWT | **Mitigado** `HttpOnly` cookie + `CSP script-src 'self'` + frontend só persiste `ana_token` em `file:` (`app/src/api.ts:139`), nunca em `https:` web |
| **Token em URL** | `GET /api/artigos?token=...` vaza em logs/referer | Token | **Bloqueado** query `token` não autentica, só `Cookie`/`Authorization: Bearer` (`handlers_auth.go:336`) |
| **Session fixation** | Atacante fixa `ana_session=fixed` antes do login | Usa sessão do atacante | **Bloqueado** login sempre gera novo JWT, não reutiliza cookie enviado (`handlers_auth.go:327`) |
| **Referer leak** | Link externo vaza `Referer: /api/artigos/1/notas` | ID/título | **Mitigado** `Referrer-Policy: no-referrer` |

## Blindagem mínima garantida (sem TLS ainda)

Mesmo em `http://127.0.0.1:8734` (desktop):
- `HttpOnly` + `SameSite=Strict` bloqueia XSS/CORS captura via JS/cross-site
- `CORS estrito` bloqueia `evil.com`/`null` em web (`ALLOWED_ORIGIN`)
- `no-store` bloqueia proxy cache
- `X-Frame DENY` + `nosniff` + `CSP` + `Permissions-Policy` ativos já em http
- `Secure` e `HSTS` ativos só com `https` (GROK ou `TLS_CERT`)

Mesmo sem TLS, um atacante **na mesma rede** ainda pode farejar http puro. Por isso:

## Como ficar minimamente blindado em rede

### Opção A — GROK (recomendado, já suportado)
```
GROK_ORIGIN=https://abc.grok.io  ALLOWED_ORIGIN=https://seu-front.web.app  BIND_ADDR=0.0.0.0  JWT_SECRET=32+bytes
# GROK encaminha https://abc.grok.io → http://127.0.0.1:8734 com X-Forwarded-Proto: https
# → cookie vira Secure, HSTS ativo, sniffing na internet não captura (TLS do GROK)
```

### Opção B — TLS direto
```
TLS_CERT=C:\certs\server.crt  TLS_KEY=C:\certs\server.key  BIND_ADDR=0.0.0.0  ALLOWED_ORIGIN=https://seu.dominio
go run . -bind=0.0.0.0  # agora https://0.0.0.0:8734 com ListenAndServeTLS (main.go:64)
# Gere self-signed: openssl req -x509 -newkey rsa:4096 -keyout server.key -out server.crt -days 365 -nodes
```

Sem A nem B e com `bind 0.0.0.0` em http puro, o log avisa:
```
AVISO CAPTURA: sem TLS (TLS_CERT/TLS_KEY vazios) tráfego em http puro pode ser capturado na rede local/WiFi. Use GROK https ou TLS
```

### O que NÃO fazer em rede sem TLS
- Não use WiFi público/aberto com `bind 0.0.0.0` http
- Não exponha `PGPASSWORD` padrão `Dudu1408@@` (alerta em main.go:42)
- Não deixe `ALLOWED_ORIGIN` vazio em `0.0.0.0` (CORS permissivo)

## Testes automatizados de captura

Arquivo `backend/internal/app/network_capture_test.go` (5 testes, todos PASS):

- `TestCapture_Sniffing_Replay` — captura cookie e replay de IP diferente funciona em http (prova risco), mas com `X-Forwarded-Proto https` cookie vira `Secure`
- `TestCapture_Cache_NoStore` — `Cache-Control` e `Pragma`
- `TestCapture_HSTS_GROK` — HSTS com/sem https
- `TestCapture_Session_Fixation` — novo token sempre
- `TestCapture_Authorization_Leak_URL` — token não vaza em query, health sem leak

Rodar: `go test -run TestCapture -v`

## Frontend

`app/src/api.ts:139` agora só persiste `ana_token` em `file:` (Electron). Em `https:` web, usa apenas `HttpOnly` cookie, fechando vetor XSS → captura via `localStorage`.

## Conclusão

**Minimamente blindado para rede**: sim, quando via `https` (GROK ou TLS direto). Em `http` puro na LAN, blindagem de aplicação está ok (CORS, HSTS, no-store, HttpOnly etc), mas **captura passiva ainda possível** — por isso o server avisa e a doc recomenda GROK/TLS para qualquer exposição `0.0.0.0`.

Se precisar de proteção extra sem TLS (ex: amarrar sessão ao IP), diga que implemento `JWT com ip_hash` + verificação em `authMiddleware`.
