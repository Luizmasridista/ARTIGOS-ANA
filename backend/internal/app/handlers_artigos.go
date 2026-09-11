package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"artigos-ana/backend/internal/pdf"
	"artigos-ana/backend/internal/store"
)

const maxUploadBytes = 50 << 20

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "versao": "0.1.0"})
}

func (a *App) handleListArtigos(w http.ResponseWriter, r *http.Request) {
	busca := strings.TrimSpace(r.URL.Query().Get("busca"))
	if len([]rune(busca)) > 200 {
		busca = string([]rune(busca)[:200])
	}
	uid, ok := getUsuarioID(r)
	if !ok || uid == 0 {
		writeErro(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	list, err := a.DB.ListArtigosByUser(busca, uid)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao listar artigos")
		return
	}
	if list == nil {
		list = []store.Artigo{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *App) handleCreateArtigo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeErro(w, http.StatusRequestEntityTooLarge, "arquivo muito grande (limite 50MB)")
			return
		}
		log.Printf("upload sem campo file: content-type=%q content-length=%d err=%v", r.Header.Get("Content-Type"), r.ContentLength, err)
		writeErro(w, http.StatusBadRequest, `campo "file" obrigatório`)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		log.Printf("upload FormFile file erro: content-type=%q err=%v", r.Header.Get("Content-Type"), err)
		writeErro(w, http.StatusBadRequest, `campo "file" obrigatório`)
		return
	}
	defer file.Close()
	// valida extensão e content-type
	ct := header.Header.Get("Content-Type")
	if ct != "" && ct != "application/pdf" && ct != "application/octet-stream" && !strings.Contains(ct, "pdf") {
		log.Printf("aviso: upload Content-Type inesperado %q", ct)
	}
	if ext := strings.ToLower(filepath.Ext(header.Filename)); ext != "" && ext != ".pdf" {
		writeErro(w, http.StatusBadRequest, "arquivo deve ter extensão .pdf")
		return
	}
	if header.Size > maxUploadBytes {
		writeErro(w, http.StatusRequestEntityTooLarge, "arquivo muito grande (limite 50MB)")
		return
	}

	head := make([]byte, 5)
	if _, err := io.ReadFull(file, head); err != nil || string(head) != "%PDF-" {
		writeErro(w, http.StatusBadRequest, "arquivo não é um PDF")
		return
	}

	titulo := strings.TrimSpace(r.FormValue("titulo"))
	if len([]rune(titulo)) > 300 {
		writeErro(w, http.StatusBadRequest, "título muito longo (máx 300 caracteres)")
		return
	}
	tituloProvisorio := false
	if titulo == "" {
		titulo = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
		tituloProvisorio = true
	}
	if titulo == "" {
		titulo = "Artigo sem título"
		tituloProvisorio = true
	}
	// sanitiza título para evitar injeção
	titulo = strings.ReplaceAll(titulo, "\n", " ")
	titulo = strings.ReplaceAll(titulo, "\r", " ")
	titulo = strings.TrimSpace(titulo)
	if titulo == "" {
		titulo = "Artigo sem título"
		tituloProvisorio = true
	}

	tmp, err := os.CreateTemp(a.tmpDir, "upload-*.pdf")
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao gravar o upload")
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, io.MultiReader(strings.NewReader(string(head)), file)); err != nil {
		tmp.Close()
		writeErro(w, http.StatusInternalServerError, "falha ao gravar o upload")
		return
	}
	tmp.Close()

	uid, ok := getUsuarioID(r)
	if !ok || uid == 0 {
		writeErro(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	id, criadoEm, err := a.DB.CreateArtigoForUser(titulo, "", uid)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao registrar o artigo")
		return
	}

	finalPDF := a.pdfPath(id)
	if err := os.Rename(tmpPath, finalPDF); err != nil {
		a.DB.DeleteArtigo(id)
		writeErro(w, http.StatusInternalServerError, "falha ao gravar o PDF")
		return
	}
	// Neon persistencia: salva pdf bytes em BYTEA para sobreviver ao disco efemero do Render Free.
	// O processamento pesado (páginas) roda no worker — a resposta volta rápido,
	// senão PDF grande estoura o timeout do proxy (100s Cloudflare) e o upload "trava".
	if pdfBytes, err := os.ReadFile(finalPDF); err == nil {
		if err := a.DB.SetArtigoPdfData(id, pdfBytes); err != nil {
			log.Printf("aviso: falha ao persistir pdf_data no Neon para artigo %d: %v", id, err)
		}
	} else {
		log.Printf("aviso: falha ao ler pdf para persistir no DB artigo %d: %v", id, err)
	}

	payload, _ := json.Marshal(map[string]any{"artigo_id": id, "titulo_provisorio": tituloProvisorio})
	job, err := a.DB.EnqueueJob(uid, "processar_pdf", payload)
	if err != nil {
		log.Printf("falha ao enfileirar processamento artigo %d: %v", id, err)
		a.DB.DeleteArtigo(id)
		os.Remove(finalPDF)
		writeErro(w, http.StatusInternalServerError, "falha ao enfileirar processamento")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          id,
		"titulo":      titulo,
		"criado_em":   criadoEm,
		"num_paginas": 0,
		"status":      "processando",
		"job_id":      job.ID,
	})
}

// processarPDFJob executa o job "processar_pdf" no worker (fora da requisição).
func (a *App) processarPDFJob(j *store.Job) error {
	var in struct {
		ArtigoID         int64 `json:"artigo_id"`
		TituloProvisorio bool  `json:"titulo_provisorio"`
	}
	if err := json.Unmarshal(j.Payload, &in); err != nil || in.ArtigoID <= 0 {
		return fmt.Errorf("payload inválido: %s", string(j.Payload))
	}
	return a.processarPDF(in.ArtigoID, j.UsuarioID, in.TituloProvisorio)
}

// processarPDF extrai páginas, persiste imagens, define o título do paper e
// varre citações. Em falha definitiva, remove o artigo (como o upload síncrono fazia).
func (a *App) processarPDF(artigoID, uid int64, tituloProvisorio bool) error {
	artigo, found, err := a.DB.GetArtigoByUser(artigoID, uid)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("artigo %d não encontrado para o usuário %d", artigoID, uid)
	}
	finalPDF := a.pdfPath(artigoID)
	// garante o PDF no disco a partir do BYTEA (disco efêmero do Render pode ter apagado)
	if _, err := os.Stat(finalPDF); err != nil {
		data, found, err := a.DB.GetArtigoPdfData(artigoID)
		if err != nil || !found || len(data) == 0 {
			return fmt.Errorf("pdf do artigo %d indisponível", artigoID)
		}
		if err := os.MkdirAll(filepath.Dir(finalPDF), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(finalPDF, data, 0o644); err != nil {
			return err
		}
	}
	_ = a.DB.SetArtigoPDF(artigoID, "pdfs/"+fmt.Sprintf("%d.pdf", artigoID))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pagesDir := a.paginasPath(artigoID)
	pages, err := pdf.Extract(ctx, a.PopplerDir, a.DataDir, finalPDF, pagesDir)
	if err != nil {
		log.Printf("extracao falhou para artigo %d: %v", artigoID, err)
		a.DB.DeleteArtigo(artigoID)
		os.Remove(finalPDF)
		os.RemoveAll(pagesDir)
		return fmt.Errorf("falha ao processar o PDF: %v", err)
	}

	for _, p := range pages {
		wordsJSON, _ := json.Marshal(p.Words)
		if wordsJSON == nil {
			wordsJSON = []byte("[]")
		}
		relImg := fmt.Sprintf("paginas/%d/%d.png", artigoID, p.Numero)
		if err := a.DB.AddPagina(artigoID, p.Numero, relImg, wordsJSON, p.WidthPx, p.HeightPx); err != nil {
			a.DB.DeleteArtigo(artigoID)
			os.Remove(finalPDF)
			os.RemoveAll(pagesDir)
			return fmt.Errorf("falha ao registrar páginas: %v", err)
		}
		// Neon persistencia: salva PNG bytes em BYTEA (fallback se disco for apagado no Render)
		if pngBytes, err := os.ReadFile(p.PNGPath); err == nil {
			if err := a.DB.SetPaginaImagemData(artigoID, p.Numero, pngBytes); err != nil {
				log.Printf("aviso: falha ao persistir imagem_data pagina %d artigo %d: %v", p.Numero, artigoID, err)
			}
		} else {
			log.Printf("aviso: falha ao ler PNG pagina %d artigo %d para DB: %v", p.Numero, artigoID, err)
		}
	}

	// Título do paper: se veio provisório (nome do arquivo), extrai da 1ª página.
	if tituloProvisorio {
		if t := a.extrairTituloPaper(finalPDF); t != "" {
			if err := a.DB.SetArtigoTitulo(artigoID, t); err != nil {
				log.Printf("aviso: falha ao gravar título do artigo %d: %v", artigoID, err)
			} else {
				artigo.Titulo = t
			}
		}
	}

	// Extração automática de citações (best-effort, não falha o job)
	if _, err := a.executarVarrerCitacoes(artigoID); err != nil {
		log.Printf("varrer citacoes automatico falhou para artigo %d: %v", artigoID, err)
	}
	return nil
}

// extrairTituloPaper devolve o título do paper a partir do texto da 1ª página.
// Heurística: primeira linha substancial que não parece cabeçalho (doi, vol, url).
// Retorna "" se não achar nada confiável (mantém o título provisório).
func (a *App) extrairTituloPaper(pdfPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	texto, err := pdf.RunPdfToTextPlain(ctx, a.PopplerDir, a.DataDir, pdfPath, 1)
	if err != nil || strings.TrimSpace(texto) == "" {
		return ""
	}
	return extrairTituloDeTexto(texto)
}

func extrairTituloDeTexto(texto string) string {
	checked := 0
	for _, ln := range strings.Split(texto, "\n") {
		ln = strings.Join(strings.Fields(ln), " ")
		if ln == "" {
			continue
		}
		checked++
		if checked > 12 {
			break
		}
		low := strings.ToLower(ln)
		// pula cabeçalho típico de periódico
		if strings.Contains(low, "doi") || strings.Contains(low, "http") ||
			strings.Contains(low, "vol.") || strings.Contains(low, "pp. ") ||
			strings.HasPrefix(low, "página ") || strings.HasPrefix(low, "page ") {
			continue
		}
		if len([]rune(ln)) < 20 {
			continue
		}
		r := []rune(ln)
		if len(r) > 200 {
			ln = strings.TrimSpace(string(r[:200]))
		}
		return ln
	}
	return ""
}

func (a *App) handleGetArtigo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	uid, _ := getUsuarioID(r)
	artigo, found, err := a.DB.GetArtigoByUser(id, uid)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao buscar o artigo")
		return
	}
	if !found {
		writeErro(w, http.StatusNotFound, "artigo não encontrado")
		return
	}
	paginas, err := a.DB.ListPaginas(id)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao buscar páginas")
		return
	}
	resumo := make([]store.PaginaResumo, 0, len(paginas))
	for _, p := range paginas {
		resumo = append(resumo, store.PaginaResumo{Numero: p.Numero, Largura: p.LarguraPx, Altura: p.AlturaPx})
	}
	writeJSON(w, http.StatusOK, store.ArtigoDetalhe{
		ID:       artigo.ID,
		Titulo:   artigo.Titulo,
		CriadoEm: artigo.CriadoEm,
		Paginas:  resumo,
	})
}

func (a *App) handleDeleteArtigo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	if _, err := a.DB.DeleteArtigo(id); err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao apagar o artigo")
		return
	}
	os.Remove(a.pdfPath(id))
	os.RemoveAll(a.paginasPath(id))
	pattern := filepath.Join(a.exportDir, fmt.Sprintf("%d-*.docx", id))
	if matches, _ := filepath.Glob(pattern); matches != nil {
		for _, m := range matches {
			os.Remove(m)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handlePaginaImagem(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	numero, ok := parseNumero(r)
	if !ok {
		writeErro(w, http.StatusBadRequest, "número de página inválido")
		return
	}
	pagina, found, err := a.DB.GetPagina(id, numero)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao buscar a página")
		return
	}
	if !found {
		writeErro(w, http.StatusNotFound, "página não encontrada")
		return
	}
	abs := a.resolveDataFile(pagina.ImagemPNG)
	if _, err := os.Stat(abs); err == nil {
		w.Header().Set("Content-Type", "image/png")
		// autenticado: private cache para não vazar entre usuários em proxy compartilhado
		w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
		http.ServeFile(w, r, abs)
		return
	}
	// fallback Neon: disco efemero do Render pode ter apagado; serve do BYTEA
	if data, found, err := a.DB.GetPaginaImagemData(id, numero); err == nil && found && len(data) > 0 {
		// best-effort: reidrata no disco para proximas requisicoes
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		_ = os.WriteFile(abs, data, 0o644)
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}
	writeErro(w, http.StatusNotFound, "imagem não encontrada")
}

func (a *App) handlePaginaCamada(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	numero, ok := parseNumero(r)
	if !ok {
		writeErro(w, http.StatusBadRequest, "número de página inválido")
		return
	}
	pagina, found, err := a.DB.GetPagina(id, numero)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao buscar a página")
		return
	}
	if !found {
		writeErro(w, http.StatusNotFound, "página não encontrada")
		return
	}
	palavras := json.RawMessage(pagina.CamadaJSON)
	if palavras == nil || string(palavras) == "null" {
		palavras = json.RawMessage("[]")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"largura":  pagina.LarguraPx,
		"altura":   pagina.AlturaPx,
		"palavras": palavras,
	})
}
