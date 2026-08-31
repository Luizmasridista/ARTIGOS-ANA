# Bindagem recomendada — Artigos Ana (web blindada)

Escolha uma das duas bindagens. Ambas deixam o app minimamente blindado para captura indireta (sniffing/replay).

## Opção A — TLS direto (LAN https, recomendada para rede local)

Tráfego criptografado na própria LAN, sem depender de GROK. Ideal para `0.0.0.0` em WiFi compartilhado.

### Gerar cert (já gerado em `backend/certs/`)

O cert self-signed para `localhost` + `127.0.0.1` + seu LAN IP (`192.168.0.19` detectado) já está em:

- `backend/certs/server.crt` (607 bytes)
- `backend/certs/server.key` (227 bytes, EC P-256)

Para regerar: `go run C:\Users\haneg\AppData\Local\Temp\gencert.go` ou `powershell backend\bin\gencert.ps1`.

### Rodar

```powershell
# 1) edite backend\.env.web (copiado de .env.web.example)
#    - JWT_SECRET 32+ bytes
#    - PGPASSWORD forte (não Dudu1408@@)
#    - ALLOWED_ORIGIN inclua https://SEU_LAN_IP:8734

# 2) rode a bindagem
powershell -ExecutionPolicy Bypass -File backend\run-web.ps1
```

O script faz: load `.env.web` → gera `JWT_SECRET` se faltar → `STRICT_CORS=1` + `BIND_ADDR=0.0.0.0` → verifica `certs/server.crt/key` → `go build -o bin\artigos-ana.exe` → `.\bin\artigos-ana.exe -bind=0.0.0.0 -port=8734` com `TLS_CERT/KEY` (https).

Logs esperados:
```
TLS ativado: cert=certs/server.crt key=certs/server.key (https)
Artigos Ana backend ouvindo em https://0.0.0.0:8734 (acessível na rede)
```

URLs: `https://localhost:8734`, `https://192.168.0.19:8734`

**Aviso do browser `NET::ERR_CERT_AUTHORITY` é normal com self-signed.** Para dev: importe `server.crt` em `certmgr.msc → Autoridades de Certificação Raiz Confíeis` ou use `chrome --ignore-certificate-errors --allow-insecure-localhost`.

Validação: `curl -k https://127.0.0.1:8734/health` deve dar `{"ok":true}` com headers `Strict-Transport-Security`, `Permissions-Policy` etc (testado em `network_capture_test.go`).

## Opção B — GROK https tunnel (sem cert local, https externo)

Use quando quer expor na internet sem gerir cert.

```powershell
# terminal 1: grok
grok http 8734 --url https://abc123.grok.io

# terminal 2: bindagem
powershell -File backend\run-grok.ps1 -GrokOrigin https://abc123.grok.io
# ou: $env:GROK_ORIGIN="https://abc123.grok.io"; .\run-grok.ps1
```

O GROK encaminha `https://abc123.grok.io → http://127.0.0.1:8734` com `X-Forwarded-Proto: https`. O backend vê `https`, seta `Secure` no cookie e `HSTS` (validado em `TestCapture_HSTS_GROK`). O tráfego na internet fica TLS, só o último salto GROK→backend é http local (seguro se GROK e backend estão na mesma máquina).

## O que a bindagem garante

- `bind 0.0.0.0` só com `ALLOWED_ORIGIN` fechado + `STRICT_CORS=1` (CORS `evil.com`/`null` bloqueados)
- `Secure` cookie só com `https` (GROK ou TLS), `HttpOnly` sempre, `SameSite=Strict`
- `Cache-Control: no-store` em `/api` (proxy não captura)
- `Rate limit 429` + `login 423` já ativos
- `HSTS` + `CSP` + `Permissions-Policy` etc em toda resposta

## Sem bindagem (http puro 0.0.0.0)

Se rodar `go run . -bind=0.0.0.0` sem `TLS_CERT` nem `GROK_ORIGIN`, o log avisa:

```
AVISO CAPTURA: sem TLS ... tráfego em http puro pode ser capturado na rede local/WiFi. Use GROK https ou TLS
```

Funciona, mas **não é minimamente blindado para captura**. Use apenas em rede cabeada confiável ou `127.0.0.1` (desktop).

## Checagem rápida pós-bindagem

```powershell
go test ./internal/app -run TestCapture -v
go test ./internal/app -run TestHardening -v
# ou ao vivo:
Invoke-WebRequest https://127.0.0.1:8734/health -SkipCertificateCheck
Invoke-WebRequest https://127.0.0.1:8734/api/info -SkipCertificateCheck # deve 401 sem auth em estrito
```
