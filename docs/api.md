# Contrato de API — Artigos Ana

Base: `http://127.0.0.1:8734/api` | JSON | Porta fixa da v1

## Autenticação — Sessão Ana Bagatinii (ADR-004)

Single-user. Usuário fixo `Ana Bagatinii` seedado em `usuarios(nome UNIQUE, senha_hash bcrypt, criado_em)`. Sessão stateless JWT HS256 autoral (stdlib `crypto/hmac`+`encoding/base64`) em cookie. Todas as rotas `/api/**` exigem sessão, exceto `GET /health`, `GET /api/health`, `GET /api/info` e `POST/GET /api/auth/*`.

### Cookie

```
ana_session=<header.payload.signature>; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=3600
```

- `Secure` sempre em prod (GROK `https` via `X-Forwarded-Proto: https`); em dev permite `ALLOW_INSECURE_COOKIE=1` para `http://127.0.0.1:5173`.
- JWT: header `{"alg":"HS256","typ":"JWT"}`, payload `{sub: id, nome: "Ana Bagatinii", exp: now+3600, iat: now}`. Segredo `JWT_SECRET` (32+ bytes) via env.

### Endpoints

- `POST /api/auth/login` body `{ "nome": "Ana Bagatinii", "senha": "..." }`
  - `200 { "ok": true }` + `Set-Cookie: ana_session=...` (login válido, zera contador do IP)
  - `401 { "erro": "credenciais inválidas" }` (sem Set-Cookie)
  - `423 { "erro": "muitas tentativas", "retryAfter": 900 }` + `Retry-After: 900` (5 falhas em 60s do mesmo IP → bloqueio 15min)
  - `400 { "erro": "nome e senha obrigatórios" }` se body vazio
- `POST /api/auth/logout` → `204` + `Set-Cookie: ana_session=; Max-Age=0; Expires=Thu, 01 Jan 1970 00:00:00 GMT` (limpa cookie; idempotente)
- `GET /api/auth/me` → `200 { "id": 1, "nome": "Ana Bagatinii" }` se cookie válido, senão `401 { "erro": "não autenticado" }` (expirado, ausente ou assinatura inválida → frontend redireciona para `Home` em `/`)

### Rate limit e auditoria

- Chave por IP (`X-Forwarded-For` primeiro IP ou `RemoteAddr`). 5 tentativas falhas em janela de 60s → 6ª retorna `423` e grava `login_tentativas(ip, tentativas, primeira_tentativa, bloqueado_ate)` com `bloqueado_ate = now+15min`. Em memória `sync.Map` para checagem rápida, PG para persistir após restart. `Retry-After` em segundos.

### Exigência de sessão nas rotas existentes

Sem cookie válido → `401 { "erro": "não autenticado" }`:

- `GET /api/artigos`, `POST /api/artigos`, `GET /api/artigos/{id}`, `DELETE /api/artigos/{id}`
- `GET /api/artigos/{id}/paginas/{numero}/imagem|/camada`
- `GET|POST /api/artigos/{id}/marcacoes`, `PATCH|DELETE /api/artigos/{id}/marcacoes/{id}`
- `GET|POST /api/artigos/{id}/notas`, `PATCH|DELETE /api/artigos/{id}/notas/{id}`
- `GET /api/artigos?busca=`, `GET /api/artigos/{id}/busca`, `GET /api/artigos/{id}/sumario`
- `GET /api/artigos/{id}/citacoes`, `POST /api/artigos/{id}/varrer-citacoes`, `POST /api/artigos/{id}/citacoes/{id}/enriquecer`, `POST /api/artigos/excluir-lote`
- `POST|GET /api/artigos/{id}/exportar`, `GET /api/artigos/{id}/historico`

Públicas (sem sessão): `GET /health`, `GET /api/health`, `GET /api/info`, `POST /api/auth/login`, `GET /api/auth/me` (retorna 401 mas não exige sessão prévia), `POST /api/auth/logout`.

Frontend: `Home.tsx` em `/` com form, `AuthContext` (`login/me/logout` via `fetch {credentials:"include"}`), `PrivateRoute` checa `GET /api/auth/me`, interceptor `401 → /`.

## Artigos

- `GET /artigos` → `[{ "id": 1, "titulo": "Artigo X", "num_paginas": 12, "criado_em": "..." }]`
- `POST /artigos` (multipart: campo `file` com o PDF, campo opcional `titulo`) → `{ "id": 1, "titulo": "...", "num_paginas": 12, "criado_em": "..." }`
- `GET /artigos/{id}` → `{ "id": 1, "titulo": "...", "criado_em": "...", "paginas": [{ "numero": 1, "largura": 1240, "altura": 1754 }] }`
- `DELETE /artigos/{id}` → `204`

## Páginas

- `GET /artigos/{id}/paginas/{numero}/imagem` → PNG da página renderizada
- `GET /artigos/{id}/paginas/{numero}/camada` → camada de texto para marcação:
  `{ "largura": 1240, "altura": 1754, "palavras": [{ "texto": "olá", "x0": 10.5, "y0": 20.1, "x1": 40.2, "y1": 32.8 }] }`
  (coordenadas em pontos do PDF, mesma unidade da imagem renderizada)

## Marcações

- `GET /artigos/{id}/marcacoes` → `[{ "id": 1, "pagina": 1, "tipo": "highlight", "cor": "#FFEB3B", "palavras": [[10.5, 20.1, 40.2, 32.8]], "texto": "trecho marcado" }]`
- `POST /artigos/{id}/marcacoes` (body: pagina, tipo, cor, palavras, texto) → objeto criado
- `PATCH /artigos/{id}/marcacoes/{marcacao_id}` (body: `{ "cor": "#9EE6A8" }`) → `204` (troca a cor; registra no histórico)
- `DELETE /artigos/{id}/marcacoes/{marcacao_id}` → `204`

## Notas

- `GET /artigos/{id}/notas` → `[{ "id": 1, "pagina": 1, "texto": "minha nota", "criado_em": "...", "marcacao_id": 3, "tags": ["revisar"], "cor": "#FFEB3B" }]` (`marcacao_id`/`tags`/`cor` opcionais; filtro `?tag=revisar&cor=#FFEB3B`)
- `POST /artigos/{id}/notas` (body: pagina, texto, opcional `marcacao_id`, `tags`, `cor`) → objeto criado
- `PATCH /artigos/{id}/notas/{nota_id}` (body: `{ "texto": "...", "tags": ["revisar"], "cor": "#FFEB3B" }`) → `200` com nota atualizada
- `DELETE /artigos/{id}/notas/{nota_id}` → `204`

## Busca

- `GET /artigos?busca=Silva` → filtra `titulo ILIKE %busca%` OR `notas.texto/tags` OR `citacoes.autor/chave` (case-insensitive)
- `GET /artigos/{id}/busca?q=atenção` → `[{ "pagina": 2, "pos": [10,20,40,30], "trecho": "... atenção ..." }]`

## Sumário

- `GET /artigos/{id}/sumario` → `[{ "titulo": "Introdução", "pagina": 1, "nivel": 1, "ordem": 1 }, { "titulo": "Metodologia", "pagina": 3, "nivel": 1, "ordem": 2 }]`

## Citações (estendido)

- `GET /artigos/{id}/citacoes` → `[{ "id":1, "tipo":"autor_ano", "chave":"(Silva, 2020)", "pagina":3, "pos":[10,20,40,30], "ocorrencias":[{ "pagina":3, "pos":[...], "trecho":"..." }] , ... }]`
- `POST /artigos/{id}/varrer-citacoes` → idem, recalcula `pagina/pos`
- `POST /api/artigos/excluir-lote` → `204`

## Exportação

- `POST /artigos/{id}/exportar` → gera o .docx, retorna `{ "caminho": "C:\\...\\data\\export\\1-artigo-x.docx", "nome": "artigo-x.docx" }`
- `GET /artigos/{id}/exportar` → download do .docx gerado (ou gera na hora se não existir)

## Histórico (controle de versão)

- `GET /artigos/{id}/historico` → lista de eventos em ordem:
  `[{ "id": 1, "entidade": "marcacao", "entidade_id": 2, "acao": "criar", "dados": { ... }, "criado_em": "..." }]`
  Toda criação, alteração e exclusão de artigos, marcações e notas gera um registro de histórico.

## Saúde

- `GET /health` → `{ "ok": true, "versao": "0.1.0" }`
- `GET /api/health` → idem (público, sem sessão)

## Segurança — headers e CORS (ADR-004)

Headers aplicados em **todas** as respostas via `securityHeaders` middleware:

- `Strict-Transport-Security: max-age=31536000; includeSubDomains` (só quando `https` — `r.TLS != nil` ou `X-Forwarded-Proto: https` do GROK)
- `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; object-src 'none'; frame-ancestors 'none'`
- `X-Frame-Options: DENY`
- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: no-referrer`

CORS (substitui `*` anterior em `backend/internal/app/helpers.go:64`):

- `Access-Control-Allow-Origin` só para `ALLOWED_ORIGIN` ou `GROK_ORIGIN` (ex: `https://abc.grok.io`) + `http://127.0.0.1:5173` em dev; sem env em prod → sem header (fechado)
- `Access-Control-Allow-Credentials: true`, `Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS`, `Allow-Headers: Content-Type`
- `OPTIONS` → `204`

## Regras

- Erros: `{ "erro": "mensagem curta" }` com status adequado (400 validação, 401 não autenticado, 423 bloqueado, 404 não encontrado, 500 interno).
- Auth: sem cookie → `401`; brute force → `423` com `Retry-After`.
- Paginação: v1 não precisa.
- O renderer chama `http://127.0.0.1:8734`; o Electron main sobe o binário Go e expõe a porta. Via GROK: `https://<id>.grok.io → http://127.0.0.1:8734` com `X-Forwarded-Proto: https`.
- Persistência: PostgreSQL local, banco `artigos_ana` (porta 5432). O backend conecta ao iniciar e desconecta ao sair; os dados continuam lá quando o app está fechado (cold down não apaga nada). Tabelas auth: `usuarios`, `login_tentativas`.
