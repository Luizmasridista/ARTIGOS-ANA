package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"artigos-ana/backend/internal/docx"
	"artigos-ana/backend/internal/pdf"
	"artigos-ana/backend/internal/store"
)

func (a *App) gerarDocx(id int64) (absPath, nome string, err error) {
	artigo, found, err := a.DB.GetArtigo(id)
	if err != nil {
		return "", "", err
	}
	if !found {
		return "", "", errArtigoNaoEncontrado
	}
	// isolamento é verificado no handler antes de chamar gerarDocx; mantém compatibilidade
	_ = artigo
	paginas, err := a.DB.ListPaginas(id)
	if err != nil {
		return "", "", err
	}
	docPages := make([]docx.PageInput, 0, len(paginas))
	for _, p := range paginas {
		docPages = append(docPages, docx.PageInput{Linhas: a.linhasDaPagina(p)})
	}
	notas, err := a.DB.ListNotas(id)
	if err != nil {
		return "", "", err
	}
	docNotas := make([]docx.NotaInput, 0, len(notas))
	for _, n := range notas {
		docNotas = append(docNotas, docx.NotaInput{Pagina: n.Pagina, Texto: n.Texto})
	}

	slug := slugify(artigo.Titulo)
	if slug == "" {
		slug = "artigo"
	}
	nome = fmt.Sprintf("%d-%s.docx", id, slug)
	absPath = filepath.Join(a.exportDir, nome)
	if err := docx.Build(absPath, docPages, docNotas); err != nil {
		return "", "", err
	}
	return absPath, nome, nil
}

func (a *App) linhasDaPagina(p store.Pagina) []docx.LinhaInput {
	layoutPath := a.resolveDataFile(strings.TrimSuffix(p.ImagemPNG, filepath.Ext(p.ImagemPNG)) + ".layout.json")
	if data, err := os.ReadFile(layoutPath); err == nil {
		var linhas []pdf.LayoutLine
		if err := json.Unmarshal(data, &linhas); err == nil {
			out := make([]docx.LinhaInput, 0, len(linhas))
			for _, l := range linhas {
				out = append(out, docx.LinhaInput{Texto: l.Texto, Tamanho: l.Tamanho, Familia: l.Familia, Cor: l.Cor})
			}
			return out
		}
	}
	var words []pdf.Word
	if err := json.Unmarshal(p.CamadaJSON, &words); err == nil {
		linhas := pdf.LinesFromWords(words)
		out := make([]docx.LinhaInput, 0, len(linhas))
		for _, l := range linhas {
			out = append(out, docx.LinhaInput{Texto: l.Texto, Tamanho: l.Tamanho})
		}
		return out
	}
	return nil
}

var errArtigoNaoEncontrado = fmt.Errorf("artigo não encontrado")

func (a *App) handleExportar(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	absPath, nome, err := a.gerarDocx(id)
	if err != nil {
		if err == errArtigoNaoEncontrado {
			writeErro(w, http.StatusNotFound, "artigo não encontrado")
			return
		}
		writeErro(w, http.StatusInternalServerError, "falha ao exportar o documento")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"caminho": absPath, "nome": nome})
}

func (a *App) handleDownloadExportar(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	absPath, nome, err := a.gerarDocx(id)
	if err != nil {
		if err == errArtigoNaoEncontrado {
			writeErro(w, http.StatusNotFound, "artigo não encontrado")
			return
		}
		writeErro(w, http.StatusInternalServerError, "falha ao exportar o documento")
		return
	}
	if _, err := os.Stat(absPath); err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao exportar o documento")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, nome))
	http.ServeFile(w, r, absPath)
}
