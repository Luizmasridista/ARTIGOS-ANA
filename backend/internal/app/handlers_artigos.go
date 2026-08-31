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
		writeErro(w, http.StatusBadRequest, `campo "file" obrigatório`)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
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
	if titulo == "" {
		titulo = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	}
	if titulo == "" {
		titulo = "Artigo sem título"
	}
	// sanitiza título para evitar injeção
	titulo = strings.ReplaceAll(titulo, "\n", " ")
	titulo = strings.ReplaceAll(titulo, "\r", " ")
	titulo = strings.TrimSpace(titulo)
	if titulo == "" {
		titulo = "Artigo sem título"
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
	_ = a.DB.SetArtigoPDF(id, "pdfs/"+fmt.Sprintf("%d.pdf", id))
	// Neon persistencia: salva pdf bytes em BYTEA para sobreviver ao disco efemero do Render Free
	if pdfBytes, err := os.ReadFile(finalPDF); err == nil {
		if err := a.DB.SetArtigoPdfData(id, pdfBytes); err != nil {
			log.Printf("aviso: falha ao persistir pdf_data no Neon para artigo %d: %v", id, err)
		}
	} else {
		log.Printf("aviso: falha ao ler pdf para persistir no DB artigo %d: %v", id, err)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	pagesDir := a.paginasPath(id)
	pages, err := pdf.Extract(ctx, a.PopplerDir, a.DataDir, finalPDF, pagesDir)
	if err != nil {
		log.Printf("extracao falhou para artigo %d: %v", id, err)
		a.DB.DeleteArtigo(id)
		os.Remove(finalPDF)
		os.RemoveAll(pagesDir)
		writeErro(w, http.StatusInternalServerError, "falha ao processar o PDF")
		return
	}

	for _, p := range pages {
		wordsJSON, _ := json.Marshal(p.Words)
		if wordsJSON == nil {
			wordsJSON = []byte("[]")
		}
		relImg := fmt.Sprintf("paginas/%d/%d.png", id, p.Numero)
		if err := a.DB.AddPagina(id, p.Numero, relImg, wordsJSON, p.WidthPx, p.HeightPx); err != nil {
			a.DB.DeleteArtigo(id)
			os.Remove(finalPDF)
			os.RemoveAll(pagesDir)
			writeErro(w, http.StatusInternalServerError, "falha ao registrar páginas")
			return
		}
		// Neon persistencia: salva PNG bytes em BYTEA (fallback se disco for apagado no Render)
		if pngBytes, err := os.ReadFile(p.PNGPath); err == nil {
			if err := a.DB.SetPaginaImagemData(id, p.Numero, pngBytes); err != nil {
				log.Printf("aviso: falha ao persistir imagem_data pagina %d artigo %d: %v", p.Numero, id, err)
			}
		} else {
			log.Printf("aviso: falha ao ler PNG pagina %d artigo %d para DB: %v", p.Numero, id, err)
		}
	}

	// Extração automática de citações (best-effort, não falha o upload)
	if _, err := a.executarVarrerCitacoes(id); err != nil {
		log.Printf("varrer citacoes automatico falhou para artigo %d: %v", id, err)
	}

	writeJSON(w, http.StatusCreated, store.Artigo{
		ID:         id,
		Titulo:     titulo,
		NumPaginas: len(pages),
		CriadoEm:   criadoEm,
	})
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
