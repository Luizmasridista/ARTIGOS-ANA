# Plano: otimizacao de envio e escaneamento de PDF em producao (Render + Neon)

Data: 2026-08-31 | Autor: techlead | Status: proposto | Produto: Artigos Ana
Stack: Go 1.26 + pgx + Neon Postgres crimson-pine-30378073 us-east-2 (pooled DATABASE_URL com -pooler, sslmode=require) + Render Free Docker 512MB/0.1CPU + poppler-utils | Frontend React 19 + Vite + PWA | Backend serve app/dist via -www

> Nota: favicon ja foi corrigido em paralelo em app/index.html, vite.config.mts e backend/internal/app/routes.go. Nao faz parte deste plano e nao e alterado aqui.

## 1. Contexto

Upload de PDF em producao esta lento e trava a tela em "escaneando". O usuario envia o arquivo e fica com spinner sem progresso ate o backend terminar tudo. Em PDFs medios de 5 MB com 20 paginas o tempo passa de 30 segundos e a conexao cai por timeout. Isso acontece so em producao no Render Free com Neon, local e rapido porque tem CPU e rede local.

Objetivo deste plano: devolver resposta rapida ao usuario em menos de 2 segundos, escanear em segundo plano, mostrar progresso e reduzir carga no Neon e no Render Free.

Referencias base:
* backend/internal/app/handlers_artigos.go:47 handleCreateArtigo (fluxo sincrono atual)
* backend/main.go:95 http.Server com WriteTimeout 30s
* backend/internal/app/app.go:81 jobSem e jobWorkerLoop com JOB_CONCURRENCY 2 e ClaimNextJob SKIP LOCKED
* backend/internal/store/store.go:157 tabela jobs e 2041 SetArtigoPdfData e 2061 SetPaginaImagemData
* backend/internal/pdf/extract.go:14 const renderDPI 150 e Extract com pdftotext + pdftoppm
* Dockerfile multi stage node:20 + golang:1.26 + debian slim + poppler-utils
* docs/adr/ADR-007-persistencia-render-neon.md (BYTEA para disco efemero)
* docs/adr/ADR-004.md (seguranca, nao quebrar governanca IP)

## 2. Diagnostico

### 2.1 Fluxo atual sincrono

1. Frontend envia POST /api/artigos multipart com campo file
2. backend/internal/app/handlers_artigos.go:48 MaxBytesReader 50 MB e ParseMultipartForm 16 MB, valida header %PDF-
3. Cria tmp em os.CreateTemp, copia bytes, chama CreateArtigoForUser, faz os.Rename para data/pdfs/{id}.pdf
4. Chama SetArtigoPDF e depois ReadFile + SetArtigoPdfData para gravar BYTEA no Neon
5. Cria context.WithTimeout 10 minutos e chama pdf.Extract que roda pdftotext -bbox e pdftoppm -png -r 150 para cada pagina, gerando PNGs em data/paginas/{id}
6. Loop por pagina: AddPagina + ReadFile PNG + SetPaginaImagemData (um UPDATE por pagina)
7. Chama executarVarrerCitacoes
8. Retorna 201 com Artigo {id, num_paginas}

Tudo ocorre dentro do mesmo request HTTP. O cliente fica bloqueado ate o passo 8.

### 2.2 Gargalos

1. Sincrono bloqueia request. O usuario so recebe resposta depois de poppler + N writes no Neon. Com 20 paginas isso e 30s a 60s no Render 0.1 CPU.
2. WriteTimeout 30s em backend/main.go:100 mata a conexao antes do fim. O context interno e 10 minutos, mas o servidor fecha o socket em 30s. Resultado: usuario ve erro de rede, mas backend continua processando e pode deixar artigo sem paginas.
3. N+1 writes BYTEA sem batch. Cada pagina faz AddPagina (INSERT) e depois SetPaginaImagemData (UPDATE com PNG de ~300 KB). Para 20 paginas sao 40 round trips ao Neon us-east-2 via pgbouncer pooled. Latencia pooled + TLS soma 100 a 300 ms por query, total 4 a 12 segundos so de banco.
4. ReadFile + SetPaginaImagemData por pagina le do disco e envia bytes grandes sem compressao e sem streaming. PNG ja vem com 150 DPI, mas ainda sao ~300 KB por pagina, 20 paginas sao ~6 MB de BYTEA. Sem batch, ocupa conexoes do pool (SetMaxOpenConns 10) e pressiona memoria 512 MB.
5. Poppler single thread sem limite. pdf.Extract roda pdftoppm pagina a pagina sequencial, sem worker pool e sem limite de paginas concorrentes. Em Render Free 0.1 CPU cada pdftoppm demora 800 ms a 1.5 s por pagina, 20 paginas sao 16 a 30 segundos de CPU.
6. Neon pooled PgBouncer nao suporta prepared statements com cache e tem limite de tamanho de pacote. UPDATE de BYTEA grande via pooled pode dar erro de prepare ou latencia extra. Ideal e usar direct connection para BYTEA ou desabilitar statement cache para essa query.
7. Sem feedback progressivo. Frontend usa request unico e spinner infinito. Nao existe polling em /api/jobs/{id} nem SSE, entao nao ha como mostrar "pagina 7 de 20".
8. Sync offline first nao cobre upload PDF. Upload ainda e online only, entao se cair a rede no meio do scan o usuario perde progresso.

### 2.3 Evidencias para coletar na Fase 0

* Logs Render: tempo entre "handleCreateArtigo inicio" e "return 201" para 3 tamanhos (1 MB 5 paginas, 5 MB 20 paginas, 20 MB 80 paginas)
* curl -w "%{time_total}" para medir p50 e p95 em producao
* EXPLAIN ANALYZE de SetArtigoPdfData e SetPaginaImagemData para ver custo de BYTEA e toast
* npx neon logs ou pg_stat_statements para ver queries mais lentas e uso de pool
* Metrica Render: CPU throttle e memoria perto de 512 MB durante pdftoppm
* Contagem de bytes: tamanho medio pdf_data e imagem_data por artigo

## 3. Objetivos e metricas

Objetivo principal: usuario recebe resposta em menos de 2 segundos e acompanha progresso sem travar.

Metricas de sucesso (com base no sintoma atual de >30s para 5 MB 20 paginas):
* p95 do POST /api/artigos para 5 MB 20 paginas: menor que 3 segundos para retornar 202 Accepted com {id, status, jobId}
* Tempo total de processamento em background para o mesmo PDF: menor que 15 segundos em Render Free (medido do EnqueueJob ate CompleteJob)
* Zero timeout por WriteTimeout 30s para upload (nenhum corte de conexao em 100 uploads de teste)
* Nenhum erro de PgBouncer pooled para BYTEA grande apos Fase 3
* go test ./... e npm run build passam sem regressao
* Frontend mostra progresso real (ex: "processando pagina 7 de 20") e estado final done ou failed com retry

Fora de escopo neste plano: trocar BYTEA por R2 ou S3 (ADR-007 ja decidiu manter BYTEA ate 10 GB), OCR, reescrita de poppler em Go, multi instanca Render.

## 4. Arquitetura proposta

### 4.1 Diagrama textual

```
[Browser React 19] 
  | POST /api/artigos (multipart 50MB max, 16MB mem)
  v
[Go net/http] -> authMiddleware -> bodyLimitMiddleware -> handleCreateArtigo (NOVO: rapido)
  | 1. valida %PDF-, cria artigo com status=processando, salva pdf em /tmp/data/pdfs/{id}.pdf
  | 2. persiste pdf_data BYTEA best effort
  | 3. EnqueueJob usuario_id, tipo=pdf_extract, payload={artigoId, pdfPath, titulo}
  | 4. retorna 202 {id, titulo, status: "processando", jobId, criado_em}
  v
[Postgres Neon jobs] <- ClaimNextJob SKIP LOCKED (jobWorkerLoop, JOB_CONCURRENCY 2)
  |
  v
[Worker pdfExtract] (goroutine por job, dentro do mesmo processo Render)
  | a. reidrata PDF do BYTEA se /tmp/data/pdfs/{id}.pdf sumiu (disco efemero)
  | b. pdf.Extract com contexto por job (timeout 10m), DPI 150, paginas sequenciais com semaforo
  | c. para cada lote de N paginas (ex: 5): Batch INSERT paginas + Batch UPDATE imagem_data
  | d. atualiza artigos status e num_paginas incremental (para polling)
  | e. varrerCitacoes best effort
  | f. CompleteJob com result {artigoId, numPaginas} ou FailJob com backoff 2^attempts ja existente
  v
[Neon artigos/paginas] -> BYTEA ja cobre persistencia sem disco

[Browser polling]
  | GET /api/jobs/{id} a cada 2s OU GET /api/artigos/{id} checando status
  | futuro: GET /api/jobs/{id}/events SSE para progresso pagina a pagina
  v
[UI] barra de progresso, estado processando/done/failed, botao tentar de novo
```

### 4.2 Contratos novos e ajustes

* POST /api/artigos muda de 201 para 202 quando entra na fila. Mantem compatibilidade com clientes antigos retornando mesmo shape mais campos novos status e jobId.
* Novo campo em artigos: status TEXT CHECK IN (pending, processing, done, failed) e job_id BIGINT e paginas_processadas INT e erro TEXT. Ou usar apenas jobs.result para status, mas artigo precisa de status para listagem rapida sem join em jobs. Decidir em ADR novo.
* GET /api/artigos/{id} passa a retornar status e paginas parciais enquanto processa.
* GET /api/jobs/{id} ja existe e sera usado para polling. Adicionar GET /api/artigos/{id}/status se preferir evitar expor jobs ao frontend.
* Worker novo: processPdfExtractJob com reidrataçao via GetArtigoPdfData se arquivo nao existe, e batch de paginas.

### 4.3 Principios

* Mesma governanca. Jobs isolados por usuario_id, polling com requireArtigoOwnership. Nao quebrar ADR-004.
* Sem novo servico externo. Worker fica no processo Go com jobSem, sem Redis.
* BYTEA continua sendo fonte de verdade para Render Free efemero. Disco e cache.
* Feedback progressivo sem polling pesado: intervalo 2s, max 60 polls, ou SSE futuro.

## 5. Fases

### Fase 0: diagnostico e instrumentacao (estimativa 0,5 dia) Owner backend

Tarefas:
* Adicionar logs com duracao em handleCreateArtigo: inicio, fim validacao, fim rename, fim SetArtigoPdfData, inicio Extract, fim Extract, fim loop paginas, fim varrerCitacoes. Usar log.Printf com id, tamanho, numPaginas, duracao ms.
* Adicionar middleware de log de tempo para POST /api/artigos
* Rodar EXPLAIN ANALYZE em SetArtigoPdfData e SetPaginaImagemData com PDF 5 MB e PNG 300 KB, anotar custo e tempo
* Medir p95 real em producao com 10 uploads 5 MB 20 paginas via curl e via frontend, anotar taxa de timeout 30s
* Verificar npx neon logs ou pg_stat_statements para queries lentas e pool wait
* Documentar baseline em docs/planos/medicoes-2026-08-31-pdf.md

Saida: tabela baseline com p50/p95/timeout e custo EXPLAIN.

Risco baixo. Nao muda comportamento.

### Fase 1: quick win sem fila (estimativa 1 dia) Owner backend

Objetivo: reduzir de >30s para ~15s sem mudar contrato, para ganhar tempo enquanto fila nao fica pronta.

Tarefas backend:
* Ajustar WriteTimeout para nao matar upload longo. Opcoes: a) aumentar WriteTimeout global de 30s para 120s em backend/main.go:100, ou b) criar Server separado para /api/artigos com timeout maior, ou c) desabilitar WriteTimeout para essa rota via handler com context. Recomendado: aumentar para 120s global ja que Render Free e single process e custo de conexao presa e baixo, e documentar em ADR. Manter ReadTimeout 15s e IdleTimeout 60s.
* Trocar loop N+1 por batch. Criar SetPaginasBatch ou usar sql.Tx com pgx.Batch para AddPagina + SetPaginaImagemData em transacao unica por lote de 5 paginas. Reduz de 40 round trips para 4.
* Garantir que renderDPI continua 150 em backend/internal/pdf/extract.go:14. Nao subir para 300, manter 150 para CPU e memoria. Se compressao extra necessaria, adicionar pngquant ou optipng como passo opcional em Fase 3.
* Limitar concorrencia de pdftoppm: mesmo sendo sequencial, garantir que so 1 Extract roda por worker e que jobSem 2 nao satura CPU 0.1. Se preciso, reduzir JOB_CONCURRENCY para 1 em Render Free via env JOB_CONCURRENCY=1.
* Adicionar limite de paginas concorrentes no Extract: ja e sequencial, manter assim para nao estourar 512 MB. Documentar limite de 200 paginas max por PDF.
* Adicionar header de progresso temporario via polling simples de GET /api/artigos/{id} retornando num_paginas parcial.

Tarefas frontend:
* Nenhuma obrigatoria nesta fase, apenas tratar timeout de fetch com retry e mensagem melhor que spinner infinito.

Validacao Fase 1:
* go test ./... passa
* Upload 5 MB 20 paginas em producao passa sem timeout 120s e tempo cai para ~15s
* Logs mostram reducao de 40 queries para 4 batches

Risco: aumentar WriteTimeout pode segurar conexao por 120s, mas com pouca carga e aceitavel. Mitiga com Fase 2 que remove request longo.

### Fase 2: fila async (estimativa 3 dias) Owner backend 2 dias + frontend 1 dia

Objetivo: resposta em <2s com 202 e processamento em background, sem timeout.

Tarefas backend:
* Migration: ALTER TABLE artigos ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','done','failed')), ADD COLUMN IF NOT EXISTS job_id BIGINT REFERENCES jobs(id), ADD COLUMN IF NOT EXISTS paginas_processadas INT DEFAULT 0, ADD COLUMN IF NOT EXISTS erro TEXT. Criar indice idx_artigos_status. Atualizar backfill para artigos existentes como done se ja tem paginas.
* Refatorar handleCreateArtigo para modo async:
  - valida e salva PDF em /tmp/data/pdfs/{id}.pdf como hoje
  - persiste pdf_data BYTEA
  - cria artigo com status pending
  - faz EnqueueJob com payload {artigoId, pdfPath, titulo, dpi:150, usuarioId}
  - retorna 202 {id, titulo, status:"processando", jobId, criado_em} imediatamente, sem Extract sincrono
  - manter compat: se header X-Sync: true, faz sincrono para testes locales (opcional)
* Criar worker pdf_extract em backend/internal/app/worker_pdf.go:
  - func processPdfExtractJob(j *store.Job) com parse payload, GetArtigoPdfData se arquivo sumiu e reidrata em /tmp
  - chama pdf.Extract com context por job (10m), atualiza artigos SET status=processing
  - para cada pagina extraida, faz batch INSERT paginas e UPDATE imagem_data por lote de 5, atualizando paginas_processadas incremental
  - ao final, UPDATE artigos SET status=done, num_paginas=len(pages), job_id=j.ID e CompleteJob com result
  - em erro, UPDATE artigos SET status=failed, erro=msg e FailJob (backoff 2^attempts ja em store.go:2033 ate 300s)
  - chamar executarVarrerCitacoes best effort antes de done
* Ajustar app.go: registrar case "pdf_extract" em processJob, manter noop/teste
* Garantir ClaimNextJob filtra por usuario_id? Hoje ClaimNextJob pega qualquer job, mas jobs tem usuario_id. Worker deve respeitar ordem global sem vazar dados entre usuarios. GetJob ja filtra por usuario_id, mas polling de progresso deve checar ownership via ArtigoOwnedBy.
* Expor GET /api/artigos/{id}/status ou reutilizar GET /api/artigos/{id} com campos novos status, paginas_processadas, job_id, erro. Adicionar GET /api/jobs/{id} ja existe para polling alternativo.

Tarefas frontend (app/src/api.ts e telas de upload):
* Criar api.criarArtigoAsync que espera 202 e retorna {id, status, jobId}
* Na tela de upload, ao receber 202, mostrar barra "processando pagina X de Y" com polling GET /api/artigos/{id} ou GET /api/jobs/{jobId} a cada 2s, max 60 tentativas, com botao cancelar que faz DELETE /api/artigos/{id}
* Tratar estados done (mostrar artigo), failed (mostrar erro + botao reprocessar que chama POST /api/artigos/{id}/reprocessar ou re-enfileira job), e pending
* Manter fallback para upload offline: se sem rede, enfileirar local e sync depois (usar mesma fila de sync offline-first ja existente, mas upload PDF continua online only nesta fase, documentar limite)

Estimativa: backend 2 dias, frontend 1 dia, total 3 dias.

### Fase 3: otimizacao Neon e Render (estimativa 2 dias) Owner backend

Tarefas:
* Batch e transacao: implementar AddPaginasTxBatch que faz pgx.Batch com INSERT INTO paginas e UPDATE paginas SET imagem_data em mesma transacao por lote. Usar statement simple protocol para pooled, ou desabilitar prepared statement para BYTEA grande. Se pooled der erro de tamanho, usar direct DATABASE_URL sem -pooler para BYTEA (via segundo pool com 2 conexoes direct, ou reuso do mesmo DSN com ?sslmode=require sem pooler).
* Habilitar statement_cache e log de pool: em store.Open ja usa pgx stdlib com SetMaxOpenConns 10, adicionar SetMaxIdleConns 5 e SetConnMaxLifetime 5m para pooled. Documentar quando usar direct para BYTEA.
* Compressao PNG: avaliar pngquant --quality=70-85 ou converter PNG para JPEG 85 para paginas de texto. Medir reducao de 300 KB para 120 KB por pagina sem perda visual grave. Se ok, adicionar em worker apos pdftoppm, antes de SetPaginaImagemData. Manter PNG para paginas com transparencia se necessario.
* Streaming e limite de memoria: em vez de os.ReadFile inteiro para pdf_data, usar io.Copy com streaming para BYTEA via pgx large object ou chunked. Para MVP, manter ReadFile mas limitar a 50 MB e monitorar memoria 512 MB. Se estourar, trocar para streaming.
* Poppler tuning: manter DPI 150, adicionar flag -aaVector yes e limitar a 1 processo por job. Documentar que Render 0.1 CPU nao suporta paralelo de paginas.
* Reidrataçao: garantir que processPdfExtractJob sempre tenta HydratePaginaImagem se os.Stat falhar, e que handlePaginaImagem ja faz fallback via GetPaginaImagemData (handler existe e esta ok).

Risco: compressao PNG pode aumentar CPU. Mitiga medindo tempo CPU antes e depois, habilitar so se ganho de rede superar custo CPU.

### Fase 4: UX e resiliencia (estimativa 1 dia) Owner frontend 0,5 dia + backend 0,5 dia

Tarefas frontend:
* Barra de progresso com SSE futuro. Para agora, polling 2s ja resolve. Preparar componente ProgressoPDF com estados: enviando (upload progress via xhr onUploadProgress), processando (polling), done, failed.
* Retry e cancel: botao tentar novamente que chama POST /api/artigos/{id}/reprocessar (novo endpoint que re-enfileira pdf_extract para mesmo artigo), e botao cancelar que chama DELETE /api/artigos/{id}
* Mostrar paginas parciais: enquanto status=processing, liberar GET /api/artigos/{id}/paginas/{n}/imagem para paginas ja processadas, para usuario comecar a ler antes do fim.

Tarefas backend:
* Endpoint POST /api/artigos/{id}/reprocessar que verifica ownership, cria novo job pdf_extract e volta status para pending
* Endpoint SSE opcional GET /api/jobs/{id}/events que envia event: progress data: {"paginas_processadas":7,"total":20} a cada pagina. Se nao fizer agora, deixar para v2, polling ja atende.
* Ajustar rate limit para polling: GET /api/jobs/{id} nao deve contar para generalRateLimit 120/min de forma agressiva. Whitelist ou limite separado 60/min por IP para jobs.
* Adicionar teste de integracao para fluxo 202 -> polling -> done.

Total plano: Fase 0 0,5 + Fase 1 1 + Fase 2 3 + Fase 3 2 + Fase 4 1 = 7,5 dias. Arredonda para 8 dias com margem.

## 6. Riscos e mitigacoes

* Render Free sem disco persistente. Mitiga ja existe com BYTEA. Worker precisa reidratar PDF do BYTEA se arquivo sumiu apos sleep 15 min. Implementar check os.Stat antes de Extract e reidratar via GetArtigoPdfData.
* Job retry com backoff 2^attempts ja existe em FailJob ate 300s. Se pdf_extract falhar por OOM 512 MB, ira para retry e pode falhar de novo. Mitiga limitando tamanho PDF 50 MB e paginas 200, e reduzindo DPI e batch.
* PgBouncer pooled nao suporta SET e prepared statements para BYTEA grande. Mitiga usando direct connection para SetArtigoPdfData e SetPaginaImagemData, ou usando exec simples sem prepare.
* Frontend polling pesado. Mitiga intervalo 2s, max 60 polls, e considerar SSE em Fase 4 para empurrar progresso.
* Quebra de compatibilidade para clientes antigos que esperam 201. Mitiga retornar 202 com mesmo shape mais campos extras, manter 201 para flag X-Sync, documentar em docs/api.md.
* Governanca IP. Nao quebrar isolamento por usuario_id. Mitiga sempre filtrar jobs por usuario_id no GetJob e checar ArtigoOwnedBy antes de retornar status.
* Render Free sleep 15 min pode matar worker no meio do Extract. Mitiga com job retry automatico e idempotencia: worker deve ser reentrante, limpar paginas parciais antes de reprocessar ou usar UPSERT por (artigo_id, numero).

## 7. Alternativas descartadas

* R2 ou S3 para PDFs e PNGs: descartado agora porque BYTEA ja resolve persistencia ate 10 GB Neon Free, sem custo extra e sem credenciais novas. ADR-007 ja decidiu. Migrar para R2 so quando passar de 8 GB ou precisar de CDN de imagens.
* Aumentar Render para plano pago com disco persistente: descartado por custo, fere requisito zero custo.
* WebSocket para progresso: descartado por complexidade, polling 2s ja atende e SSE e mais simples que WebSocket para fluxo unidirecional.
* Processar PDF no frontend com WASM (pdf.js): descartado porque poppler ja existe e extrai camada JSONB com coordenadas precisas para marcacoes, refazer em WASM perderia precisao e aumentaria bundle.
* Trocar poppler por lib Go pura (ex: unidoc): descartado por licenca e fidelidade, poppler via apt ja esta no Dockerfile e funciona em debian slim.
* Fila externa Redis ou BullMQ: descartado porque jobs ja existe com SKIP LOCKED no Postgres, sem infra extra, suficiente para JOB_CONCURRENCY 2.

## 8. Validacao

### Comandos obrigatorios

```bash
# backend
go vet ./...
go test ./... -run Test -count=1
go test ./... -run TestJobs -v

# frontend
npm --prefix app install
npm --prefix app run build
npx --prefix app tsc --noEmit

# banco
# conectar via DATABASE_URL pooled e direct, rodar:
EXPLAIN ANALYZE UPDATE artigos SET pdf_data = $1 WHERE id = $1;
EXPLAIN ANALYZE UPDATE paginas SET imagem_data = $1 WHERE artigo_id = $1 AND numero = $2;
# verificar tamanho
SELECT pg_size_pretty(pg_total_relation_size('artigos')), pg_size_pretty(pg_total_relation_size('paginas'));
SELECT avg(octet_length(pdf_data)) as avg_pdf, avg(octet_length(imagem_data)) as avg_png FROM artigos LEFT JOIN paginas ON paginas.artigo_id = artigos.id;

# producao
curl -w "@curl-format.txt" -X POST -H "Cookie: ana_session=..." -F "file=@5mb-20paginas.pdf" https://artigos-ana.onrender.com/api/artigos -i
# esperar 202, depois
curl https://artigos-ana.onrender.com/api/jobs/{id} -H "Cookie: ana_session=..." | jq
curl https://artigos-ana.onrender.com/api/artigos/{id} -H "Cookie: ana_session=..." | jq .status

# logs Neon
npx neon logs --project crimson-pine-30378073 --branch main | grep -i "error\|timeout\|pool"
```

### Testes a criar

* backend/internal/app/handlers_artigos_async_test.go: POST /api/artigos retorna 202 em <500 ms sem chamar Extract, job criado com status pending
* backend/internal/app/worker_pdf_test.go: processPdfExtractJob reidrata PDF do BYTEA quando arquivo falta, cria paginas em batch, atualiza status done, chama varrerCitacoes
* backend/internal/store/jobs_test.go: EnqueueJob + ClaimNextJob SKIP LOCKED com 2 workers concorrentes nao pega mesmo job
* app/src/api.test.ts: criarArtigoAsync polling 2s ate done, trata failed com retry
* e2e: upload 5 MB 20 paginas em Render preview, verifica 202 em <3s, polling ate done em <15s, sem timeout, imagem pagina 1 disponivel antes do fim

### Criterios de pronto

* POST /api/artigos com 5 MB 20 paginas retorna 202 em <3s p95 em producao
* Worker processa em <15s e artigo fica done com paginas disponiveis
* Nenhum timeout 30s em 50 uploads seguidos
* go vet, go test, npm run build verdes
* npx neon logs sem erro de pool ou BYTEA

## 9. ADRs necessarios

* ADR-008: upload async com jobs pdf_extract e status em artigos, contrato 202 e polling. Referencia ADR-007 para persistencia BYTEA.
* Atualizar docs/api.md com novo fluxo 202, GET /api/jobs/{id} e GET /api/artigos/{id} com status
* Atualizar docs/adr/ADR-004 se mudar rate limit para polling

## 10. Proximos passos imediatos

1. Rodar Fase 0 hoje: adicionar logs de duracao em handlers_artigos.go e medir baseline real em producao com 3 PDFs de tamanhos diferentes, anotar EXPLAIN ANALYZE e tamanho BYTEA.
2. Implementar Fase 1 amanha: aumentar WriteTimeout para 120s em backend/main.go e trocar loop N+1 por batch de 5 paginas em store.go, validar em preview Render que timeout some.
3. Iniciar Fase 2 em seguida: criar migration de status em artigos, refatorar handleCreateArtigo para 202 + EnqueueJob e implementar worker_pdf.go com reidrataçao do BYTEA, com frontend polling a cada 2s.

## 11. Anexos

* Arquivos chave: backend/internal/app/handlers_artigos.go:47, backend/main.go:95, backend/internal/app/app.go:81, backend/internal/store/store.go:157 e 2041, backend/internal/pdf/extract.go:14, Dockerfile, render.yaml, app/src/api.ts
* Env: DATABASE_URL pooled com -pooler e sslmode=require, JOB_CONCURRENCY 2 (sugerir 1 em Render Free), SYNC_CONCURRENCY 4
* Limites: maxUploadBytes 50 MB, ParseMultipartForm 16 MB, renderDPI 150, WriteTimeout 30s (a corrigir), pdf context 10m

