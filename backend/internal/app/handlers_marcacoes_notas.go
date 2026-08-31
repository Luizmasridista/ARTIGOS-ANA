package app

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"artigos-ana/backend/internal/store"
)

type marcacaoInput struct {
	Pagina   int         `json:"pagina"`
	Tipo     string      `json:"tipo"`
	Cor      string      `json:"cor"`
	Palavras [][]float64 `json:"palavras"`
	Texto    string      `json:"texto"`
}

func (a *App) handleListMarcacoes(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	list, err := a.DB.ListMarcacoes(id)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao listar marcações")
		return
	}
	if list == nil {
		list = []store.Marcacao{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *App) handleCreateMarcacao(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	var in marcacaoInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeErro(w, http.StatusRequestEntityTooLarge, "corpo muito grande")
			return
		}
		writeErro(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if in.Pagina < 1 {
		writeErro(w, http.StatusBadRequest, "página inválida")
		return
	}
	if in.Texto == "" {
		writeErro(w, http.StatusBadRequest, "texto obrigatório")
		return
	}
	if in.Cor == "" {
		writeErro(w, http.StatusBadRequest, "cor obrigatória")
		return
	}
	if in.Tipo == "" {
		in.Tipo = "highlight"
	}
	palavrasJSON, err := json.Marshal(in.Palavras)
	if err != nil || palavrasJSON == nil {
		palavrasJSON = []byte("[]")
	}
	uid, _ := getUsuarioID(r)
	m, err := a.DB.CreateMarcacaoForUser(id, uid, in.Pagina, in.Tipo, in.Cor, palavrasJSON, in.Texto)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao criar marcação")
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (a *App) handleDeleteMarcacao(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	marcacaoID, ok := parseID(r, "marcacao_id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	deleted, err := a.DB.DeleteMarcacao(id, marcacaoID)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao apagar marcação")
		return
	}
	if !deleted {
		writeErro(w, http.StatusNotFound, "marcação não encontrada")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type marcacaoUpdateInput struct {
	Cor string `json:"cor"`
}

func (a *App) handleUpdateMarcacao(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	marcacaoID, ok := parseID(r, "marcacao_id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	var in marcacaoUpdateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeErro(w, http.StatusRequestEntityTooLarge, "corpo muito grande")
			return
		}
		writeErro(w, http.StatusBadRequest, "JSON inválido: informe a cor")
		return
	}
	if in.Cor == "" {
		writeErro(w, http.StatusBadRequest, "JSON inválido: informe a cor")
		return
	}
	updated, err := a.DB.UpdateMarcacaoCor(id, marcacaoID, in.Cor)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao atualizar marcação")
		return
	}
	if !updated {
		writeErro(w, http.StatusNotFound, "marcação não encontrada")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type notaInput struct {
	Pagina     int      `json:"pagina"`
	Texto      string   `json:"texto"`
	MarcacaoID *int64   `json:"marcacao_id"`
	Tags       []string `json:"tags"`
	Cor        string   `json:"cor"`
}

var reCorHex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func sanitizeTags(in []string) ([]string, bool) {
	var out []string
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if strings.Contains(t, ",") || strings.Contains(t, ";") {
			return nil, false
		}
		if len([]rune(t)) > 50 {
			return nil, false
		}
		out = append(out, t)
	}
	if out == nil {
		return []string{}, true
	}
	// dedup preserve order
	seen := map[string]bool{}
	var dedup []string
	for _, t := range out {
		if !seen[t] {
			seen[t] = true
			dedup = append(dedup, t)
		}
	}
	return dedup, true
}

func (a *App) handleListNotas(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	tagFilter := strings.TrimSpace(r.URL.Query().Get("tag"))
	corFilter := strings.TrimSpace(r.URL.Query().Get("cor"))
	if corFilter != "" && !reCorHex.MatchString(corFilter) {
		writeErro(w, http.StatusBadRequest, "cor inválida")
		return
	}
	if tagFilter != "" && (strings.Contains(tagFilter, ",") || strings.Contains(tagFilter, ";")) {
		writeErro(w, http.StatusBadRequest, "tag inválida")
		return
	}
	var list []store.Nota
	var err error
	if tagFilter != "" || corFilter != "" {
		list, err = a.DB.ListNotasFiltered(id, tagFilter, corFilter)
	} else {
		list, err = a.DB.ListNotas(id)
	}
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao listar notas")
		return
	}
	if list == nil {
		list = []store.Nota{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *App) handleCreateNota(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	var in notaInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeErro(w, http.StatusRequestEntityTooLarge, "corpo muito grande")
			return
		}
		writeErro(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if in.Pagina < 1 {
		writeErro(w, http.StatusBadRequest, "página inválida")
		return
	}
	if strings.TrimSpace(in.Texto) == "" {
		writeErro(w, http.StatusBadRequest, "texto obrigatório")
		return
	}
	if len([]rune(in.Texto)) > 10000 {
		writeErro(w, http.StatusBadRequest, "texto muito longo")
		return
	}
	tags, okTags := sanitizeTags(in.Tags)
	if !okTags {
		writeErro(w, http.StatusBadRequest, "tag inválida")
		return
	}
	cor := strings.TrimSpace(in.Cor)
	if cor == "" {
		cor = "#FFEB3B"
	}
	if !reCorHex.MatchString(cor) {
		writeErro(w, http.StatusBadRequest, "cor inválida")
		return
	}
	uid, _ := getUsuarioID(r)
	n, err := a.DB.CreateNotaComTagsForUser(id, uid, in.Pagina, in.Texto, in.MarcacaoID, tags, cor)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao criar nota")
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

func (a *App) handleUpdateNota(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	notaID, ok := parseID(r, "nota_id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeErro(w, http.StatusRequestEntityTooLarge, "corpo muito grande")
			return
		}
		writeErro(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if len(raw) == 0 {
		writeErro(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	var textoPtr *string
	var tagsPtr *[]string
	var corPtr *string
	if v, ok := raw["texto"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			writeErro(w, http.StatusBadRequest, "texto inválido")
			return
		}
		sTrim := strings.TrimSpace(s)
		if sTrim == "" {
			writeErro(w, http.StatusBadRequest, "texto inválido")
			return
		}
		if len([]rune(s)) > 10000 {
			writeErro(w, http.StatusBadRequest, "texto muito longo")
			return
		}
		textoPtr = &s
	}
	if v, ok := raw["tags"]; ok {
		var arr []string
		if err := json.Unmarshal(v, &arr); err != nil {
			writeErro(w, http.StatusBadRequest, "tags inválido")
			return
		}
		san, okSan := sanitizeTags(arr)
		if !okSan {
			writeErro(w, http.StatusBadRequest, "tag inválida")
			return
		}
		tagsPtr = &san
	}
	if v, ok := raw["cor"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			writeErro(w, http.StatusBadRequest, "cor inválida")
			return
		}
		s = strings.TrimSpace(s)
		if !reCorHex.MatchString(s) {
			writeErro(w, http.StatusBadRequest, "cor inválida")
			return
		}
		corPtr = &s
	}
	if textoPtr == nil && tagsPtr == nil && corPtr == nil {
		writeErro(w, http.StatusBadRequest, "nenhum campo para atualizar")
		return
	}
	updated, found, err := a.DB.UpdateNota(id, notaID, textoPtr, tagsPtr, corPtr)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao atualizar nota")
		return
	}
	if !found {
		writeErro(w, http.StatusNotFound, "nota não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *App) handleDeleteNota(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	notaID, ok := parseID(r, "nota_id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	deleted, err := a.DB.DeleteNota(id, notaID)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao apagar nota")
		return
	}
	if !deleted {
		writeErro(w, http.StatusNotFound, "nota não encontrada")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) artigoExiste(w http.ResponseWriter, id int64) bool {
	_, found, err := a.DB.GetArtigo(id)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao buscar o artigo")
		return false
	}
	if !found {
		writeErro(w, http.StatusNotFound, "artigo não encontrado")
		return false
	}
	return true
}

func (a *App) artigoExisteOwned(w http.ResponseWriter, r *http.Request, id int64) bool {
	return a.requireArtigoOwnership(w, r, id)
}
