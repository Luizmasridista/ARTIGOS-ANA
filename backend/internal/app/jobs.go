package app

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (a *App) handleEnqueueJob(w http.ResponseWriter, r *http.Request) {
	uid, ok := getUsuarioID(r)
	if !ok || uid == 0 {
		writeErro(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	var in struct {
		Tipo    string          `json:"tipo"`
		Payload json.RawMessage `json:"payload"`
	}
	if !decodeJSONLimit(w, r, &in) {
		return
	}
	in.Tipo = strings.TrimSpace(in.Tipo)
	if in.Tipo == "" {
		writeErro(w, http.StatusBadRequest, "tipo obrigatório")
		return
	}
	if len(in.Tipo) > 100 {
		writeErro(w, http.StatusBadRequest, "tipo muito longo")
		return
	}
	if in.Payload == nil {
		in.Payload = json.RawMessage(`{}`)
	}
	var tmp json.RawMessage
	if err := json.Unmarshal(in.Payload, &tmp); err != nil {
		if !json.Valid(in.Payload) {
			writeErro(w, http.StatusBadRequest, "payload JSON inválido")
			return
		}
	}
	job, err := a.DB.EnqueueJob(uid, in.Tipo, in.Payload)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao enfileirar job")
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (a *App) handleGetJob(w http.ResponseWriter, r *http.Request) {
	uid, ok := getUsuarioID(r)
	if !ok || uid == 0 {
		writeErro(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	job, found, err := a.DB.GetJob(uid, id)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao buscar job")
		return
	}
	if !found {
		writeErro(w, http.StatusNotFound, "job não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (a *App) handleListJobs(w http.ResponseWriter, r *http.Request) {
	uid, ok := getUsuarioID(r)
	if !ok || uid == 0 {
		writeErro(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	list, err := a.DB.ListJobs(uid, 20)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao listar jobs")
		return
	}
	writeJSON(w, http.StatusOK, list)
}
