# ADR-007 — Persistencia PDFs/PNGs no Neon + Deploy Render Free (2026-08-30)

Status: aceito | Data: 2026-08-30 | Autor: backend

## Contexto

Render Free tem disco efemero (apagado a cada restart/sleep 15min). Backend salvava PDFs em `data/pdfs/%d.pdf` e PNGs em `data/paginas/%d/*.png` (`handlers_artigos.go:126`, `store.go` schema). Apos restart, DB mantinha linha `artigos`/`paginas` mas `os.Stat` falhava -> `GET /api/artigos/{id}/paginas/{n}/imagem` 404. `main.go` lia `BIND_ADDR` mas nao `PORT` (Render injeta `PORT=10000`), bind ficava em 8734. Frontend ja serve via `-www` mesma origem (`app/src/api.ts:19`). DB Neon Free 10GB disponivel via `DATABASE_URL` (`postgres://...neon.tech/...?sslmode=require`).

## Decisao

**Persistencia barata sem novo servico (sem R2/S3): BYTEA no Postgres.**

- Schema: `ALTER TABLE artigos ADD COLUMN IF NOT EXISTS pdf_data BYTEA` + `ALTER TABLE paginas ADD COLUMN IF NOT EXISTS imagem_data BYTEA` (`store.go`). Nullable para migracao, lazy load.
- Upload (`handleCreateArtigo`): apos `os.Rename` para `finalPDF`, `os.ReadFile` + `SetArtigoPdfData`; para cada pagina apos `AddPagina`, `SetPaginaImagemData`. Best-effort com log, nao falha upload se DB falhar.
- Serve (`handlePaginaImagem`): tenta `os.Stat` primeiro (dev local rapido). Se ausente, `GetPaginaImagemData` do DB, reidrata no disco (`WriteFile`) e serve `w.Write` com `Content-Type image/png` + `private, max-age=86400`. Mantem compat disco local.
- Helpers `store.SetArtigoPdfData/GetArtigoPdfData/SetPaginaImagemData/GetPaginaImagemData/HydratePaginaImagem` em `store.go`.
- `pdf/extract.go`: `resolvePopplerBin` tenta `popplerDir/pdftotext(.exe)` + `LookPath` para funcionar em `bin/poppler` (Windows) e `/usr/bin` (Debian poppler-utils). Sem quebrar dev.
- `main.go`: le `PORT` env (`strconv.Atoi`) apos `flag.Parse` e antes de `srv.Addr`; fallback 8734. Tambem le `DATABASE_URL` env como fallback a `-db-url`. Documenta `BIND_ADDR=0.0.0.0` obrigatorio em Render.
- `Dockerfile` multi-stage: `node:20-bookworm-slim` (npm ci && npm run build -> /app/www), `golang:1.26-bookworm` (go build -o /app/bin/artigos-ana), `debian:bookworm-slim` (apt poppler-utils ca-certificates). `EXPOSE 10000`, `ENV BIND_ADDR=0.0.0.0 PORT=10000`, `CMD ["/app/bin/artigos-ana","-bind=0.0.0.0","-port=10000","-www=/app/www","-data=/tmp/data","-poppler=/usr/bin"]`. `/tmp/data` efemero avisado, DB cobre.
- `render.yaml`: `type web env docker plan free dockerfilePath ./Dockerfile healthCheckPath /health` com `DATABASE_URL sync:false`, `JWT_SECRET generateValue:true`, `ALLOWED_ORIGIN sync:false`, `BIND_ADDR 0.0.0.0`, `PORT 10000`.
- `.dockerignore` e `.env.web.example` documentam `DATABASE_URL`.

## Consequencias

- Upload 50MB cabe em BYTEA (Neon 10GB, ~200 PDFs grandes). Imagens ~300KB/pagina, 10 paginas ~3MB. Sem custo extra, sem credenciais R2.
- `go vet`/`go test` passam offline (sem DB, sync_unit). Upload local continua via disco; apos `rm data/pdfs` imagem ainda 200 via DB.
- Tradeoff: BYTEA aumenta dump/backup e latencia vs object storage; aceitavel ate 10GB. Migracao futura para R2 e so trocar helpers (interface isolada).
- Render sleep 15min nao perde dados; cold start reidrata do DB sob demanda.

## Alternativas

- **R2/S3**: rejeitado agora — requer bucket + credenciais + presigned URL, custo/complexidade desnecessaria para MVP free tier.
- **Render Disk pago ($0.25/GB)**: rejeitado — quebra requisito zero custo.
- **Volume Neondb separado**: nao existe; BYTEA e o mais simples.

## Validacao

- `go vet ./...` ok
- `go test ./...` ok (skip DB se sem Postgres)
- `npm run build` gera `app/dist/index.html` copiado para `/app/www`
- `main.go` loga `ouvindo em 0.0.0.0:PORT` quando `BIND_ADDR=0.0.0.0`
- `rm -rf data/pdfs data/paginas && GET /api/artigos/{id}/paginas/1/imagem -> 200` via DB
- `docker build -t test .` syntax ok; `render.yaml` tem `healthCheckPath /health`, `DATABASE_URL sync:false`, `JWT_SECRET generateValue:true`
