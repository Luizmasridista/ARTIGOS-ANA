package app

import (
	"net/http"

	"artigos-ana/backend/internal/store"
)

func (a *App) handleGetHistorico(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	list, err := a.DB.ListHistorico(id)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao buscar o histórico")
		return
	}
	if list == nil {
		list = []store.Historico{}
	}
	writeJSON(w, http.StatusOK, list)
}
