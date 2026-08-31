# Plano — Leitor de Artigos PDF com Notas e Exportação para DOC

Data: 2026-08-27 | Orquestrador: orquestrador | Status: em execução | Produto: Artigos Ana (nome provisório)

## 1. Objetivo

Criar uma plataforma (mini CRUD) onde o usuário sobe um artigo em PDF, o sistema escaneia e exibe o artigo, e dentro dela ele pode marcar texto e tomar notas. O artigo pode ser transferido para um arquivo DOC (Word/Google Docs) preservando o conteúdo visual (fontes, tipografia, marcas d'água, layout). O usuário escolhe: editar/notar no Word ou Google Docs, ou anotar direto na nossa plataforma.

## 2. Escopo

### Entra na v1
- [ ] Upload de PDF e cadastro do artigo (CRUD: criar, listar, abrir, apagar)
- [ ] Extração do conteúdo: texto, fontes e renderização página a página
- [ ] Visualizador de páginas no app
- [ ] Marcação de texto (highlight) e notas por página, salvas no banco
- [x] Exportar para .docx com conteúdo EDITÁVEL (texto com fonte/tamanho) + notas no final + quebras de página
- [ ] Download do .docx para abrir no Word ou importar no Google Docs
- [ ] PDFs nativos digitais (com texto embutido)

### Fica fora da v1
- [ ] OCR de PDF escaneado (v2)
- [ ] Integração direta com Google Docs via OAuth (v2; na v1 o usuário importa o .docx)
- [ ] Login/multiusuário (v1 é usuário local único)
- [ ] Edição do texto do artigo dentro da plataforma (v1 só anota sobre o original; edição de texto é no Word/Docs)

## 3. Fluxos do usuário

1. Abre a plataforma e sobe um PDF (drag and drop ou botão).
2. O sistema extrai o artigo e mostra as páginas.
3. O usuário seleciona um trecho e aplica marcação (cor de destaque) e/ou cria uma nota na página.
4. As marcações e notas ficam salvas; ele pode reabrir o artigo depois.
5. Clicou em "Exportar para DOC": o sistema gera um .docx com as páginas preservadas (fonte, tipografia, marca d'água) e as notas na margem/final.
6. O usuário baixa o arquivo e escolhe: editar no Word, importar no Google Docs, ou continuar anotando na plataforma.

## 4. Arquitetura (rascunho)

- **App desktop:** Electron (Windows), com React + Vite + TypeScript no renderer.
- **Backend:** Go, simples (stdlib + chi ou gin leve). Extração e render de páginas via poppler-utils (pdftotext/pdftoppm) chamados pelo Go. Geração do .docx em Go (go-docx ou OOXML direto). Banco PostgreSQL local (porta 5432, banco `artigos_ana`) via `pgx` stdlib.
- **Comunicação:** o Electron main sobe o binário do backend Go em localhost (porta fixa) e o React consome a API.
- **Exportação editável:** estratégia v2 (ADR-001): texto reconstruído como parágrafos editáveis com fonte, tamanho e cor (`pdftotext -bbox-layout`); sem imagens coladas; notas no final com âncora "Página N".
- **Arquivos:** PDFs originais e páginas renderizadas em pasta local `data/`; banco PostgreSQL para metadados, marcações, notas e histórico.

## 5. Modelo de dados (rascunho)

- `artigo`: id, titulo, arquivo_pdf, criado_em
- `pagina`: id, artigo_id, numero, imagem_png, texto (camada extraída/OCR)
- `marcacao`: id, artigo_id, pagina_id, tipo (highlight/subline), cor, coords, texto_selecionado
- `nota`: id, artigo_id, pagina_id, texto, criado_em
- `historico`: id, artigo_id, entidade (artigo/pagina/marcacao/nota), entidade_id, acao (criar/atualizar/excluir), dados (JSONB), criado_em — toda criação, alteração e exclusão de artigo, marcação e nota gera um registro

## 6. UX (diretriz)

- **Público:** estudantes. **Tom:** profissional/business, nível ferramenta de freelancer (produtividade séria, sem cara de brinquedo).
- **Referências de estilo:** Notion/Linear — interface limpa, tipografia forte, espaçamento generoso, sem excesso de cor.
- **Princípios:**
  - Abrir o app e já estar a um clique de subir um artigo.
  - Marcar texto com no máximo 2 cliques (selecionar, escolher cor/nota).
  - Lista de artigos simples com busca; notas sempre acessíveis num painel lateral.
  - Estados vazios úteis ("Arraste um PDF aqui") e feedback claro de exportação (progresso + "abrir pasta").
  - Modo claro e escuro com alternância (segue o sistema como padrão inicial).
- **Design tokens e componentes** ficam em `docs/` (a definir pelo frontend no início da implementação).

## 7. ADRs pendentes (techlead)

- [ ] ADR-001: estratégia de exportação fiel (página-imagem + camada de texto vs. reconstrução do texto formatado). Recomendação do orquestrador: híbrida.
- [ ] ADR-002: stack — decidido com o usuário: Electron + React no app, Go no backend. Techlead valida bibliotecas (chi/gin, go-docx vs OOXML direto, modernc.org/sqlite) e formaliza no ADR.
- [ ] ADR-003: contrato de API entre backend e frontend

## 8. Tarefas por agente

Ordem: techlead primeiro (decisões + contrato), depois backend e frontend em paralelo, depois integração e validação.

### Techlead
- [x] 1. Formalizar ADR-001 e ADR-002 em `docs/adr/` (validando poppler-utils, lib DOCX e SQLite puro Go)
- [x] 2. Escrever contrato da API em `docs/api.md` (endpoints: artigo, página, marcação, nota, exportação)

### Backend (depende de 1 e 2)
- [x] 3. API CRUD de artigos + upload de PDF (Go, SQLite → PostgreSQL)
- [x] 4. Pipeline de extração: pdftotext/pdftoppm (poppler) chamados pelo Go + render de páginas em PNG
- [x] 5. Endpoints de marcações e notas (+ histórico/controle de versão em PostgreSQL)
- [x] 6. Exportação DOCX: páginas-imagem em tamanho real + camada de texto pesquisável + seção de notas

### Frontend (depende de 2; roda em paralelo com backend)
- [x] 7. Shell Electron + React com design system (tokens/componentes conforme seção 6)
- [x] 8. Tela de upload e listagem de artigos (estados vazios, busca)
- [x] 9. Visualizador de páginas com painel lateral de notas
- [x] 10. Marcação de texto (seleção + highlight) e notas por página
- [x] 11. Botão de exportação com progresso + download do .docx ("abrir pasta")

### Integração e qualidade (techlead + orquestrador)
- [x] 12. Testar o fluxo completo com um artigo real (upload, camada, marcação, nota, histórico, exportação)
- [x] 13. Verificar DOCX gerado no Word (aberto de verdade, 4 páginas, texto pesquisável) — Google Docs fica por conta do usuário (importar o .docx manualmente)
- [x] 14. Passada de UX com os princípios da seção 6 e ajustes finais

## 9. Critérios de aceitação

- [ ] Subir PDF com texto: páginas aparecem fiéis ao original (fonte, layout, marca d'água visíveis)
- [ ] Marcar texto e criar nota: dados persistem após reabrir o artigo
- [x] Exportar DOCX: abre no Word e no Google Docs com o conteúdo EDITÁVEL (texto de verdade, com fonte/tamanho) e notas presentes
- [ ] Fluxo do usuário funciona sem linha de comando

## 10. Riscos

- **Fidelidade total PDF→DOCX:** impossível 100% por extração de texto; mitigado com página-imagem + camada de texto (fidelidade visual garantida, edição de texto fica no Word/Docs).
- **PDFs com layout complexo (colunas/tabelas):** a imagem preserva o visual; a camada de texto pode perder ordem.
- **OCR (v2):** qualidade depende do scan; usar Tesseract com treino pt-br.
- **Tamanho do DOCX:** páginas em imagem deixam o arquivo pesado; aceitável na v1, otimizar depois (compressão JPEG por página).
- **Dependência do poppler no Windows:** precisar empacotar pdftotext/pdftoppm junto do app (distribuir binários ou fallback de lib pura Go, decisão do techlead no ADR-002).

## 11. Decisões do usuário (fechadas)

- [x] Stack: app desktop Electron + backend em Go (simples)
- [x] Forma de rodar: app local desktop (Electron)
- [x] UX: foco business/profissional para estudantes (seção 6)
- [x] Google Docs na v1: via importação manual do .docx

## 12. Perguntas pendentes para o usuário

- [x] Modo escuro e claro, com alternância (segue o sistema como padrão inicial)
- [x] Nome provisório do produto: Artigos Ana
