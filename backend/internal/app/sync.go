package app

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"artigos-ana/backend/internal/store"
)

// sync: offline-first, servidor-autoritativo
// Conflitos detectados via baseVersion (versao do cliente vs versao do servidor).
// Politica: se baseVersion vier e nao for a versao atual, NAO aplica a mutacao; retorna status "conflict" com dados atuais do servidor para o cliente reconciliar e reenviar com nova versao.
// NAO confiar em relogio do cliente: updated_at sempre vem do servidor (now()).
// Isolamento: sync e por usuario_id (do JWT). Endpoints legados permanecem globais por compatibilidade (ver comentario em store.go backfillSyncColumns).
// Limite claro: reescrita de endpoints legados para isolamento completo esta fora desta fatia.

type syncOperation struct {
	OpID        string          `json:"opId"`
	ClientID    string          `json:"clientId"`
	DeviceID    string          `json:"deviceId"`
	Entity      string          `json:"entity"`
	Action      string          `json:"action"`
	Data        json.RawMessage `json:"data"`
	BaseVersion *int64          `json:"baseVersion"`
	ID          *int64          `json:"id"`
	ArtigoID    *int64          `json:"artigo_id"`
	ArtigoIdAlt *int64          `json:"artigoId"`
}

type syncRequest struct {
	DeviceID   string          `json:"deviceId"`
	Operations []syncOperation `json:"operations"`
}

type syncResult struct {
	OpID       string          `json:"opId"`
	Status     string          `json:"status"` // applied | already_applied | conflict | error
	ID         *int64          `json:"id,omitempty"`
	ClientID   *string         `json:"clientId,omitempty"`
	Version    *int64          `json:"version,omitempty"`
	Error      *string         `json:"error,omitempty"`
	ServerData json.RawMessage `json:"serverData,omitempty"`
}

type syncResponse struct {
	Results []syncResult `json:"results"`
	Cursor  string       `json:"cursor"`
}

// cursor opaco: base64url(JSON{t:unixNano, e:entity, i:maxID})
// e ausente = compatibilidade com cursor antigo {t,i}. cursor vazio = epoch.
type syncCursor struct {
	T int64  `json:"t"`
	E string `json:"e,omitempty"`
	I int64  `json:"i"`
}

func encodeCursor(t time.Time, entity string, id int64) string {
	c := syncCursor{T: t.UnixNano(), E: entity, I: id}
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (time.Time, string, int64, error) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, "", 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", 0, fmt.Errorf("cursor inválido")
	}
	var c syncCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return time.Time{}, "", 0, fmt.Errorf("cursor inválido")
	}
	return time.Unix(0, c.T), c.E, c.I, nil
}

func getUsuarioID(r *http.Request) (int64, bool) {
	v := r.Context().Value(ctxUserIDKey)
	if v == nil {
		return 0, false
	}
	uid, ok := v.(int64)
	return uid, ok
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint") || strings.Contains(msg, "23505")
}

func (a *App) handleSync(w http.ResponseWriter, r *http.Request) {
	uid, ok := getUsuarioID(r)
	if !ok || uid == 0 {
		writeErro(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if !a.tryAcquireSyncSlot() {
		writeErro(w, http.StatusTooManyRequests, "sync ocupado, tente novamente")
		return
	}
	defer a.releaseSyncSlot()

	var req syncRequest
	if !decodeJSONLimit(w, r, &req) {
		return
	}
	if len(req.Operations) == 0 {
		writeErro(w, http.StatusBadRequest, "operations vazio")
		return
	}
	if len(req.Operations) > 100 {
		writeErro(w, http.StatusBadRequest, "lote muito grande (máx 100)")
		return
	}
	deviceID := strings.TrimSpace(req.DeviceID)
	results := make([]syncResult, 0, len(req.Operations))
	var maxTime time.Time
	var maxEntity string
	var maxID int64
	for _, op := range req.Operations {
		res, updatedAt, updatedEntity, updatedID := a.processSyncOp(uid, deviceID, op)
		results = append(results, res)
		if res.Status == "applied" || res.Status == "conflict" {
			// ordena por (updatedAt, entity, id) para determinismo
			if updatedAt.After(maxTime) || (updatedAt.Equal(maxTime) && (updatedEntity > maxEntity || (updatedEntity == maxEntity && updatedID > maxID))) {
				maxTime = updatedAt
				maxEntity = updatedEntity
				maxID = updatedID
			}
		}
	}
	cursor := ""
	if !maxTime.IsZero() {
		cursor = encodeCursor(maxTime, maxEntity, maxID)
	}
	writeJSON(w, http.StatusOK, syncResponse{Results: results, Cursor: cursor})
}

func (a *App) processSyncOp(uid int64, defaultDeviceID string, op syncOperation) (syncResult, time.Time, string, int64) {
	opID := strings.TrimSpace(op.OpID)
	if opID == "" {
		msg := "opId obrigatório"
		return syncResult{OpID: op.OpID, Status: "error", Error: &msg}, time.Time{}, "", 0
	}
	if op.Entity != "nota" && op.Entity != "marcacao" && op.Entity != "artigo" {
		msg := "entity inválida (use nota|marcacao|artigo)"
		return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, "", 0
	}
	if op.Action != "create" && op.Action != "update" && op.Action != "delete" {
		msg := "action inválida (use create|update|delete)"
		return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, "", 0
	}
	deviceID := strings.TrimSpace(op.DeviceID)
	if deviceID == "" {
		deviceID = defaultDeviceID
	}
	// idempotencia rapida fora da transacao (best effort)
	if existing, found, _ := a.DB.GetSyncOperation(uid, opID); found {
		var prev syncResult
		_ = json.Unmarshal(existing, &prev)
		prev.Status = "already_applied"
		prev.OpID = opID
		return prev, time.Time{}, "", 0
	}
	tx, err := a.DB.DB().Begin()
	if err != nil {
		msg := "falha ao iniciar transação"
		return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, "", 0
	}
	defer tx.Rollback()
	// serialize concorrencia para o mesmo usuario+opId
	lockKey := fmt.Sprintf("%d:%s", uid, opID)
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, lockKey); err != nil {
		msg := "falha ao serializar operação"
		return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, "", 0
	}
	// revalida dentro da transacao sob lock
	var existsCheck bool
	_ = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM sync_operations WHERE usuario_id = $1 AND op_id = $2)`, uid, opID).Scan(&existsCheck)
	if existsCheck {
		_ = tx.Commit()
		if existing, found, _ := a.DB.GetSyncOperation(uid, opID); found {
			var prev syncResult
			_ = json.Unmarshal(existing, &prev)
			prev.Status = "already_applied"
			prev.OpID = opID
			return prev, time.Time{}, "", 0
		}
		msg := "already_applied"
		return syncResult{OpID: opID, Status: "already_applied", Error: &msg}, time.Time{}, "", 0
	}
	var res syncResult
	var updatedAt time.Time
	var updatedEntity string
	var updatedID int64
	switch op.Entity {
	case "nota":
		res, updatedAt, updatedID = a.processSyncNotaTx(tx, uid, op)
		updatedEntity = "nota"
	case "marcacao":
		res, updatedAt, updatedID = a.processSyncMarcacaoTx(tx, uid, op)
		updatedEntity = "marcacao"
	case "artigo":
		res, updatedAt, updatedID = a.processSyncArtigoTx(tx, uid, op)
		updatedEntity = "artigo"
	}
	if res.Status == "error" {
		_ = tx.Rollback()
		return res, time.Time{}, "", 0
	}
	respBytes, _ := json.Marshal(res)
	if err := a.DB.PutSyncOperationTx(tx, uid, opID, deviceID, json.RawMessage(respBytes)); err != nil {
		if isUniqueViolation(err) {
			_ = tx.Rollback()
			if existing, found, _ := a.DB.GetSyncOperation(uid, opID); found {
				var prev syncResult
				_ = json.Unmarshal(existing, &prev)
				prev.Status = "already_applied"
				prev.OpID = opID
				return prev, time.Time{}, "", 0
			}
			msg := "already_applied"
			return syncResult{OpID: opID, Status: "already_applied", Error: &msg}, time.Time{}, "", 0
		}
		_ = tx.Rollback()
		msg := "falha ao persistir idempotência"
		return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, "", 0
	}
	if err := tx.Commit(); err != nil {
		if isUniqueViolation(err) {
			if existing, found, _ := a.DB.GetSyncOperation(uid, opID); found {
				var prev syncResult
				_ = json.Unmarshal(existing, &prev)
				prev.Status = "already_applied"
				prev.OpID = opID
				return prev, time.Time{}, "", 0
			}
		}
		msg := "falha ao commitar"
		return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, "", 0
	}
	return res, updatedAt, updatedEntity, updatedID
}

func validateArtigoOwnershipTx(tx *sql.Tx, artigoID, uid int64) error {
	var deletedAt sql.NullTime
	err := tx.QueryRow(`SELECT deleted_at FROM artigos WHERE id = $1 AND usuario_id = $2`, artigoID, uid).Scan(&deletedAt)
	if err == sql.ErrNoRows {
		return fmt.Errorf("artigo não encontrado")
	}
	if err != nil {
		return err
	}
	if deletedAt.Valid {
		return fmt.Errorf("artigo já excluído")
	}
	return nil
}

func validateMarcacaoOwnershipTx(tx *sql.Tx, marcacaoID, artigoID, uid int64) error {
	var artID int64
	var userID int64
	var deletedAt sql.NullTime
	err := tx.QueryRow(`SELECT artigo_id, usuario_id, deleted_at FROM marcacoes WHERE id = $1`, marcacaoID).Scan(&artID, &userID, &deletedAt)
	if err == sql.ErrNoRows {
		return fmt.Errorf("marcação não encontrada")
	}
	if err != nil {
		return err
	}
	if deletedAt.Valid {
		return fmt.Errorf("marcação já excluída")
	}
	if userID != uid {
		return fmt.Errorf("marcação não pertence ao usuário")
	}
	if artID != artigoID {
		return fmt.Errorf("marcação não pertence ao mesmo artigo")
	}
	return nil
}

func (a *App) processSyncNotaTx(tx *sql.Tx, uid int64, op syncOperation) (syncResult, time.Time, int64) {
	opID := op.OpID
	clientID := strings.TrimSpace(op.ClientID)
	switch op.Action {
	case "create":
		var data struct {
			ArtigoID    *int64   `json:"artigo_id"`
			ArtigoIdAlt *int64   `json:"artigoId"`
			Pagina      int      `json:"pagina"`
			Texto       string   `json:"texto"`
			MarcacaoID  *int64   `json:"marcacao_id"`
			Tags        []string `json:"tags"`
			Cor         string   `json:"cor"`
		}
		if len(op.Data) > 0 {
			if err := json.Unmarshal(op.Data, &data); err != nil {
				msg := "data inválido"
				return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
			}
		}
		artigoID := int64(0)
		if data.ArtigoID != nil {
			artigoID = *data.ArtigoID
		} else if data.ArtigoIdAlt != nil {
			artigoID = *data.ArtigoIdAlt
		}
		if artigoID == 0 && op.ArtigoID != nil {
			artigoID = *op.ArtigoID
		} else if artigoID == 0 && op.ArtigoIdAlt != nil {
			artigoID = *op.ArtigoIdAlt
		}
		if artigoID == 0 {
			msg := "artigo_id obrigatório"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if err := validateArtigoOwnershipTx(tx, artigoID, uid); err != nil {
			msg := err.Error()
			if msg == "artigo não encontrado" || msg == "artigo já excluído" {
				return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
			}
			msg2 := "falha ao validar artigo"
			return syncResult{OpID: opID, Status: "error", Error: &msg2}, time.Time{}, 0
		}
		if data.Pagina < 1 {
			msg := "pagina inválida"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if strings.TrimSpace(data.Texto) == "" {
			msg := "texto obrigatório"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if clientID == "" {
			clientID = "sync-nota-" + opID
		}
		if existing, _ := a.DB.GetNotaSyncByClientIDTx(tx, clientID, uid); existing != nil {
			v := existing.Version
			id := existing.ID
			cid := existing.ClientID
			return syncResult{OpID: opID, Status: "already_applied", ID: &id, ClientID: &cid, Version: &v}, existing.UpdatedAt, existing.ID
		}
		tags, ok := sanitizeTags(data.Tags)
		if !ok {
			msg := "tag inválida"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		cor := strings.TrimSpace(data.Cor)
		if cor == "" {
			cor = "#FFEB3B"
		}
		if data.MarcacaoID != nil {
			if err := validateMarcacaoOwnershipTx(tx, *data.MarcacaoID, artigoID, uid); err != nil {
				msg := err.Error()
				return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
			}
		}
		n, err := a.DB.InsertNotaSyncTx(tx, uid, artigoID, clientID, data.Pagina, data.Texto, data.MarcacaoID, tags, cor)
		if err != nil {
			if isUniqueViolation(err) {
				if existing, _ := a.DB.GetNotaSyncByClientIDTx(tx, clientID, uid); existing != nil {
					v := existing.Version
					id := existing.ID
					cid := existing.ClientID
					return syncResult{OpID: opID, Status: "already_applied", ID: &id, ClientID: &cid, Version: &v}, existing.UpdatedAt, existing.ID
				}
			}
			msg := "falha ao criar nota"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		v := n.Version
		id := n.ID
		cid := n.ClientID
		return syncResult{OpID: opID, Status: "applied", ID: &id, ClientID: &cid, Version: &v}, n.UpdatedAt, n.ID
	case "update":
		var data struct {
			Pagina     *int      `json:"pagina"`
			Texto      *string   `json:"texto"`
			MarcacaoID *int64    `json:"marcacao_id"`
			Tags       *[]string `json:"tags"`
			Cor        *string   `json:"cor"`
		}
		if len(op.Data) > 0 {
			_ = json.Unmarshal(op.Data, &data)
		}
		var target *store.NotaSync
		var err error
		if clientID != "" {
			target, err = a.DB.GetNotaSyncByClientIDTx(tx, clientID, uid)
		} else if op.ID != nil {
			target, err = a.DB.GetNotaSyncByIDTx(tx, *op.ID, uid)
		} else {
			msg := "clientId ou id obrigatório para update"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if err != nil {
			msg := "falha ao buscar nota"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if target == nil {
			msg := "nota não encontrada"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if target.Deleted {
			msg := "nota já excluída (tombstone)"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		// servidor-autoritativo: se baseVersion conflita, NAO aplica mutacao
		if op.BaseVersion != nil && *op.BaseVersion != target.Version {
			serverData, _ := json.Marshal(target)
			v := target.Version
			id := target.ID
			cid := target.ClientID
			return syncResult{OpID: opID, Status: "conflict", ID: &id, ClientID: &cid, Version: &v, ServerData: serverData}, target.UpdatedAt, target.ID
		}
		if data.MarcacaoID != nil {
			if err := validateMarcacaoOwnershipTx(tx, *data.MarcacaoID, target.ArtigoID, uid); err != nil {
				msg := err.Error()
				return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
			}
		}
		var tagsPtr *[]string
		if data.Tags != nil {
			san, ok := sanitizeTags(*data.Tags)
			if !ok {
				msg := "tag inválida"
				return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
			}
			tagsPtr = &san
		}
		if data.Pagina == nil && data.Texto == nil && data.Tags == nil && data.Cor == nil && data.MarcacaoID == nil {
			msg := "nenhum campo para atualizar"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		updated, err := a.DB.UpdateNotaSyncTx(tx, target.ID, uid, data.Pagina, data.Texto, data.MarcacaoID, tagsPtr, data.Cor)
		if err != nil {
			msg := "falha ao atualizar nota"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		v := updated.Version
		id := target.ID
		cid := target.ClientID
		return syncResult{OpID: opID, Status: "applied", ID: &id, ClientID: &cid, Version: &v}, updated.UpdatedAt, target.ID
	case "delete":
		var target *store.NotaSync
		var err error
		if clientID != "" {
			target, err = a.DB.GetNotaSyncByClientIDTx(tx, clientID, uid)
		} else if op.ID != nil {
			target, err = a.DB.GetNotaSyncByIDTx(tx, *op.ID, uid)
		} else {
			msg := "clientId ou id obrigatório para delete"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if err != nil {
			msg := "falha ao buscar nota"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if target == nil {
			msg := "nota não encontrada"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if target.Deleted {
			v := target.Version
			id := target.ID
			cid := target.ClientID
			return syncResult{OpID: opID, Status: "already_applied", ID: &id, ClientID: &cid, Version: &v}, target.UpdatedAt, target.ID
		}
		if op.BaseVersion != nil && *op.BaseVersion != target.Version {
			serverData, _ := json.Marshal(target)
			v := target.Version
			id := target.ID
			cid := target.ClientID
			return syncResult{OpID: opID, Status: "conflict", ID: &id, ClientID: &cid, Version: &v, ServerData: serverData}, target.UpdatedAt, target.ID
		}
		version, updatedAt, err := a.DB.SoftDeleteNotaSyncTx(tx, target.ID, uid)
		if err != nil {
			if err == sql.ErrNoRows {
				v := target.Version
				id := target.ID
				cid := target.ClientID
				return syncResult{OpID: opID, Status: "already_applied", ID: &id, ClientID: &cid, Version: &v}, target.UpdatedAt, target.ID
			}
			msg := "falha ao deletar nota"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		id := target.ID
		cid := target.ClientID
		return syncResult{OpID: opID, Status: "applied", ID: &id, ClientID: &cid, Version: &version}, updatedAt, target.ID
	}
	msg := "ação não suportada"
	return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
}

func (a *App) processSyncMarcacaoTx(tx *sql.Tx, uid int64, op syncOperation) (syncResult, time.Time, int64) {
	opID := op.OpID
	clientID := strings.TrimSpace(op.ClientID)
	switch op.Action {
	case "create":
		var data struct {
			ArtigoID    *int64      `json:"artigo_id"`
			ArtigoIdAlt *int64      `json:"artigoId"`
			Pagina      int         `json:"pagina"`
			Tipo        string      `json:"tipo"`
			Cor         string      `json:"cor"`
			Palavras    [][]float64 `json:"palavras"`
			Texto       string      `json:"texto"`
		}
		if len(op.Data) > 0 {
			if err := json.Unmarshal(op.Data, &data); err != nil {
				msg := "data inválido"
				return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
			}
		}
		artigoID := int64(0)
		if data.ArtigoID != nil {
			artigoID = *data.ArtigoID
		} else if data.ArtigoIdAlt != nil {
			artigoID = *data.ArtigoIdAlt
		}
		if artigoID == 0 && op.ArtigoID != nil {
			artigoID = *op.ArtigoID
		} else if artigoID == 0 && op.ArtigoIdAlt != nil {
			artigoID = *op.ArtigoIdAlt
		}
		if artigoID == 0 {
			msg := "artigo_id obrigatório"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if err := validateArtigoOwnershipTx(tx, artigoID, uid); err != nil {
			msg := err.Error()
			if msg == "artigo não encontrado" || msg == "artigo já excluído" {
				return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
			}
			msg2 := "falha ao validar artigo"
			return syncResult{OpID: opID, Status: "error", Error: &msg2}, time.Time{}, 0
		}
		if data.Pagina < 1 {
			msg := "pagina inválida"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if strings.TrimSpace(data.Texto) == "" {
			msg := "texto obrigatório"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if strings.TrimSpace(data.Cor) == "" {
			msg := "cor obrigatória"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if data.Tipo == "" {
			data.Tipo = "highlight"
		}
		if clientID == "" {
			clientID = "sync-marcacao-" + opID
		}
		if existing, _ := a.DB.GetMarcacaoSyncByClientIDTx(tx, clientID, uid); existing != nil {
			v := existing.Version
			id := existing.ID
			cid := existing.ClientID
			return syncResult{OpID: opID, Status: "already_applied", ID: &id, ClientID: &cid, Version: &v}, existing.UpdatedAt, existing.ID
		}
		palJSON, _ := json.Marshal(data.Palavras)
		if palJSON == nil {
			palJSON = []byte("[]")
		}
		m, err := a.DB.InsertMarcacaoSyncTx(tx, uid, artigoID, clientID, data.Pagina, data.Tipo, data.Cor, palJSON, data.Texto)
		if err != nil {
			if isUniqueViolation(err) {
				if existing, _ := a.DB.GetMarcacaoSyncByClientIDTx(tx, clientID, uid); existing != nil {
					v := existing.Version
					id := existing.ID
					cid := existing.ClientID
					return syncResult{OpID: opID, Status: "already_applied", ID: &id, ClientID: &cid, Version: &v}, existing.UpdatedAt, existing.ID
				}
			}
			msg := "falha ao criar marcação"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		v := m.Version
		id := m.ID
		cid := m.ClientID
		return syncResult{OpID: opID, Status: "applied", ID: &id, ClientID: &cid, Version: &v}, m.UpdatedAt, m.ID
	case "update":
		var data struct {
			Cor      *string      `json:"cor"`
			Texto    *string      `json:"texto"`
			Palavras *[][]float64 `json:"palavras"`
		}
		if len(op.Data) > 0 {
			_ = json.Unmarshal(op.Data, &data)
		}
		var target *store.MarcacaoSync
		var err error
		if clientID != "" {
			target, err = a.DB.GetMarcacaoSyncByClientIDTx(tx, clientID, uid)
		} else if op.ID != nil {
			target, err = a.DB.GetMarcacaoSyncByIDTx(tx, *op.ID, uid)
		} else {
			msg := "clientId ou id obrigatório para update"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if err != nil {
			msg := "falha ao buscar marcação"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if target == nil {
			msg := "marcação não encontrada"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if target.Deleted {
			msg := "marcação já excluída (tombstone)"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if op.BaseVersion != nil && *op.BaseVersion != target.Version {
			serverData, _ := json.Marshal(target)
			v := target.Version
			id := target.ID
			cid := target.ClientID
			return syncResult{OpID: opID, Status: "conflict", ID: &id, ClientID: &cid, Version: &v, ServerData: serverData}, target.UpdatedAt, target.ID
		}
		var palBytes *[]byte
		if data.Palavras != nil {
			b, _ := json.Marshal(*data.Palavras)
			palBytes = &b
		}
		if data.Cor == nil && data.Texto == nil && palBytes == nil {
			msg := "nenhum campo para atualizar"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		updated, err := a.DB.UpdateMarcacaoSyncTx(tx, target.ID, uid, data.Cor, data.Texto, palBytes)
		if err != nil {
			msg := "falha ao atualizar marcação"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		v := updated.Version
		id := target.ID
		cid := target.ClientID
		return syncResult{OpID: opID, Status: "applied", ID: &id, ClientID: &cid, Version: &v}, updated.UpdatedAt, target.ID
	case "delete":
		var target *store.MarcacaoSync
		var err error
		if clientID != "" {
			target, err = a.DB.GetMarcacaoSyncByClientIDTx(tx, clientID, uid)
		} else if op.ID != nil {
			target, err = a.DB.GetMarcacaoSyncByIDTx(tx, *op.ID, uid)
		} else {
			msg := "clientId ou id obrigatório para delete"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if err != nil {
			msg := "falha ao buscar marcação"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if target == nil {
			msg := "marcação não encontrada"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		if target.Deleted {
			v := target.Version
			id := target.ID
			cid := target.ClientID
			return syncResult{OpID: opID, Status: "already_applied", ID: &id, ClientID: &cid, Version: &v}, target.UpdatedAt, target.ID
		}
		if op.BaseVersion != nil && *op.BaseVersion != target.Version {
			serverData, _ := json.Marshal(target)
			v := target.Version
			id := target.ID
			cid := target.ClientID
			return syncResult{OpID: opID, Status: "conflict", ID: &id, ClientID: &cid, Version: &v, ServerData: serverData}, target.UpdatedAt, target.ID
		}
		version, updatedAt, err := a.DB.SoftDeleteMarcacaoSyncTx(tx, target.ID, uid)
		if err != nil {
			if err == sql.ErrNoRows {
				v := target.Version
				id := target.ID
				cid := target.ClientID
				return syncResult{OpID: opID, Status: "already_applied", ID: &id, ClientID: &cid, Version: &v}, target.UpdatedAt, target.ID
			}
			msg := "falha ao deletar marcação"
			return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
		}
		id := target.ID
		cid := target.ClientID
		return syncResult{OpID: opID, Status: "applied", ID: &id, ClientID: &cid, Version: &version}, updatedAt, target.ID
	}
	msg := "ação não suportada"
	return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
}

func (a *App) processSyncArtigoTx(tx *sql.Tx, uid int64, op syncOperation) (syncResult, time.Time, int64) {
	opID := op.OpID
	clientID := strings.TrimSpace(op.ClientID)
	if op.Action != "create" {
		msg := "artigo sync só suporta create nesta fatia"
		return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
	}
	var data struct {
		Titulo     string `json:"titulo"`
		ArquivoPDF string `json:"arquivo_pdf"`
	}
	if len(op.Data) > 0 {
		_ = json.Unmarshal(op.Data, &data)
	}
	if strings.TrimSpace(data.Titulo) == "" {
		msg := "titulo obrigatório"
		return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
	}
	if clientID == "" {
		clientID = "sync-artigo-" + opID
	}
	var existsID int64
	var existsVersion int64
	var existsUpdated time.Time
	err := tx.QueryRow(`SELECT id, version, updated_at FROM artigos WHERE client_id = $1 AND usuario_id = $2`, clientID, uid).Scan(&existsID, &existsVersion, &existsUpdated)
	if err == nil {
		v := existsVersion
		cid := clientID
		return syncResult{OpID: opID, Status: "already_applied", ID: &existsID, ClientID: &cid, Version: &v}, existsUpdated, existsID
	}
	var id int64
	var version int64
	var updatedAt time.Time
	criado := time.Now()
	err = tx.QueryRow(`INSERT INTO artigos (titulo, arquivo_pdf, criado_em, usuario_id, client_id, version, updated_at) VALUES ($1,$2,$3,$4,$5,1,now()) RETURNING id, version, updated_at`, data.Titulo, data.ArquivoPDF, criado, uid, clientID).Scan(&id, &version, &updatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			_ = tx.QueryRow(`SELECT id, version, updated_at FROM artigos WHERE client_id = $1 AND usuario_id = $2`, clientID, uid).Scan(&existsID, &existsVersion, &existsUpdated)
			if existsID != 0 {
				v := existsVersion
				cid := clientID
				return syncResult{OpID: opID, Status: "already_applied", ID: &existsID, ClientID: &cid, Version: &v}, existsUpdated, existsID
			}
		}
		msg := "falha ao criar artigo"
		return syncResult{OpID: opID, Status: "error", Error: &msg}, time.Time{}, 0
	}
	v := version
	cid := clientID
	return syncResult{OpID: opID, Status: "applied", ID: &id, ClientID: &cid, Version: &v}, updatedAt, id
}

// pull

type syncPullRequest struct {
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

type syncPullResponse struct {
	Changes []syncChange `json:"changes"`
	Cursor  string       `json:"cursor"`
	HasMore bool         `json:"hasMore"`
}

type syncChange struct {
	Entity    string          `json:"entity"`
	ID        int64           `json:"id"`
	ClientID  string          `json:"clientId"`
	Version   int64           `json:"version"`
	UpdatedAt string          `json:"updatedAt"`
	Deleted   bool            `json:"deleted"`
	Data      json.RawMessage `json:"data,omitempty"`
}

func (a *App) handleSyncPull(w http.ResponseWriter, r *http.Request) {
	uid, ok := getUsuarioID(r)
	if !ok || uid == 0 {
		writeErro(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	var cursorStr string
	limit := 50
	if r.Method == http.MethodGet {
		cursorStr = r.URL.Query().Get("cursor")
		if lStr := r.URL.Query().Get("limit"); lStr != "" {
			if l, err := strconv.Atoi(lStr); err == nil && l > 0 && l <= 100 {
				limit = l
			}
		}
	} else {
		var req syncPullRequest
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				cursorStr = req.Cursor
				if req.Limit > 0 && req.Limit <= 100 {
					limit = req.Limit
				}
			}
		}
	}
	cursorTime, cursorEntity, cursorID, err := decodeCursor(cursorStr)
	if err != nil {
		writeErro(w, http.StatusBadRequest, "cursor inválido")
		return
	}
	changes, nextCursor, hasMore, err := a.fetchSyncChanges(uid, cursorTime, cursorEntity, cursorID, limit)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao buscar alterações")
		return
	}
	resp := syncPullResponse{Changes: changes, Cursor: nextCursor, HasMore: hasMore}
	if resp.Changes == nil {
		resp.Changes = []syncChange{}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) fetchSyncChanges(uid int64, cursorTime time.Time, cursorEntity string, cursorID int64, limit int) ([]syncChange, string, bool, error) {
	type row struct {
		entity    string
		id        int64
		clientID  string
		version   int64
		updatedAt time.Time
		deleted   bool
		data      json.RawMessage
	}
	var rows []row
	useLegacy := cursorEntity == "" && cursorID != 0 && !cursorTime.IsZero()
	if cursorTime.IsZero() {
		cursorTime = time.Unix(0, 0)
	}
	// notas
	{
		var rs *sql.Rows
		var err error
		if useLegacy {
			rs, err = a.DB.DB().Query(`SELECT id, COALESCE(client_id,''), version, updated_at, deleted_at IS NOT NULL, artigo_id, pagina, texto, marcacao_id, COALESCE(tags::TEXT,'{}'), COALESCE(cor,'#FFEB3B') FROM notas WHERE usuario_id = $1 AND (updated_at > $2 OR (updated_at = $2 AND id > $3)) ORDER BY updated_at, id LIMIT $4`, uid, cursorTime, cursorID, limit)
		} else {
			rs, err = a.DB.DB().Query(`SELECT id, COALESCE(client_id,''), version, updated_at, deleted_at IS NOT NULL, artigo_id, pagina, texto, marcacao_id, COALESCE(tags::TEXT,'{}'), COALESCE(cor,'#FFEB3B') FROM notas WHERE usuario_id = $1 AND (updated_at, 'nota'::text, id) > ($2, $3::text, $4) ORDER BY updated_at, id LIMIT $5`, uid, cursorTime, cursorEntity, cursorID, limit)
			if cursorEntity == "" && cursorID == 0 {
				// tuple with "" already covers initial; ensure correct query when cursor vazio inicial
				// reexec com fallback simples se necessario; ja cobre
			}
		}
		if err != nil {
			return nil, "", false, err
		}
		for rs.Next() {
			var id, version int64
			var clientID string
			var updatedAt time.Time
			var deleted bool
			var artigoID int64
			var pagina int
			var texto string
			var marcacaoID sql.NullInt64
			var tagsRaw sql.NullString
			var cor sql.NullString
			if err := rs.Scan(&id, &clientID, &version, &updatedAt, &deleted, &artigoID, &pagina, &texto, &marcacaoID, &tagsRaw, &cor); err != nil {
				rs.Close()
				return nil, "", false, err
			}
			tags := []string{}
			if tagsRaw.Valid {
				tags = decodeTagsLocal(tagsRaw.String)
			}
			corStr := "#FFEB3B"
			if cor.Valid {
				corStr = cor.String
			}
			dataObj := map[string]any{"pagina": pagina, "texto": texto, "tags": tags, "cor": corStr, "artigo_id": artigoID}
			if marcacaoID.Valid {
				dataObj["marcacao_id"] = marcacaoID.Int64
			}
			b, _ := json.Marshal(dataObj)
			rows = append(rows, row{entity: "nota", id: id, clientID: clientID, version: version, updatedAt: updatedAt, deleted: deleted, data: b})
		}
		rs.Close()
	}
	// marcacoes
	{
		var rs *sql.Rows
		var err error
		if useLegacy {
			rs, err = a.DB.DB().Query(`SELECT id, COALESCE(client_id,''), version, updated_at, deleted_at IS NOT NULL, artigo_id, pagina, tipo, cor, texto, palavras_json FROM marcacoes WHERE usuario_id = $1 AND (updated_at > $2 OR (updated_at = $2 AND id > $3)) ORDER BY updated_at, id LIMIT $4`, uid, cursorTime, cursorID, limit)
		} else {
			rs, err = a.DB.DB().Query(`SELECT id, COALESCE(client_id,''), version, updated_at, deleted_at IS NOT NULL, artigo_id, pagina, tipo, cor, texto, palavras_json FROM marcacoes WHERE usuario_id = $1 AND (updated_at, 'marcacao'::text, id) > ($2, $3::text, $4) ORDER BY updated_at, id LIMIT $5`, uid, cursorTime, cursorEntity, cursorID, limit)
		}
		if err != nil {
			return nil, "", false, err
		}
		for rs.Next() {
			var id, version int64
			var clientID string
			var updatedAt time.Time
			var deleted bool
			var artigoID int64
			var pagina int
			var tipo, cor, texto string
			var palavras []byte
			if err := rs.Scan(&id, &clientID, &version, &updatedAt, &deleted, &artigoID, &pagina, &tipo, &cor, &texto, &palavras); err != nil {
				rs.Close()
				return nil, "", false, err
			}
			dataObj := map[string]any{"pagina": pagina, "tipo": tipo, "cor": cor, "texto": texto, "palavras": json.RawMessage(palavras), "artigo_id": artigoID}
			b, _ := json.Marshal(dataObj)
			rows = append(rows, row{entity: "marcacao", id: id, clientID: clientID, version: version, updatedAt: updatedAt, deleted: deleted, data: b})
		}
		rs.Close()
	}
	// artigos
	{
		var rs *sql.Rows
		var err error
		if useLegacy {
			rs, err = a.DB.DB().Query(`SELECT id, COALESCE(client_id,''), version, updated_at, deleted_at IS NOT NULL, titulo, arquivo_pdf FROM artigos WHERE usuario_id = $1 AND (updated_at > $2 OR (updated_at = $2 AND id > $3)) ORDER BY updated_at, id LIMIT $4`, uid, cursorTime, cursorID, limit)
		} else {
			rs, err = a.DB.DB().Query(`SELECT id, COALESCE(client_id,''), version, updated_at, deleted_at IS NOT NULL, titulo, arquivo_pdf FROM artigos WHERE usuario_id = $1 AND (updated_at, 'artigo'::text, id) > ($2, $3::text, $4) ORDER BY updated_at, id LIMIT $5`, uid, cursorTime, cursorEntity, cursorID, limit)
		}
		if err != nil {
			return nil, "", false, err
		}
		for rs.Next() {
			var id, version int64
			var clientID string
			var updatedAt time.Time
			var deleted bool
			var titulo, arquivoPDF string
			if err := rs.Scan(&id, &clientID, &version, &updatedAt, &deleted, &titulo, &arquivoPDF); err != nil {
				rs.Close()
				return nil, "", false, err
			}
			dataObj := map[string]any{"titulo": titulo, "arquivo_pdf": arquivoPDF}
			b, _ := json.Marshal(dataObj)
			rows = append(rows, row{entity: "artigo", id: id, clientID: clientID, version: version, updatedAt: updatedAt, deleted: deleted, data: b})
		}
		rs.Close()
	}
	// ordena por updatedAt, entity, id
	for i := 1; i < len(rows); i++ {
		j := i
		for j > 0 && (rows[j].updatedAt.Before(rows[j-1].updatedAt) || (rows[j].updatedAt.Equal(rows[j-1].updatedAt) && (rows[j].entity < rows[j-1].entity || (rows[j].entity == rows[j-1].entity && rows[j].id < rows[j-1].id)))) {
			rows[j], rows[j-1] = rows[j-1], rows[j]
			j--
		}
	}
	hasMore := false
	if len(rows) > limit {
		rows = rows[:limit]
		hasMore = true
	} else if len(rows) == limit {
		last := rows[len(rows)-1]
		var cnt int
		if useLegacy {
			_ = a.DB.DB().QueryRow(`SELECT (SELECT COUNT(*) FROM notas WHERE usuario_id = $1 AND (updated_at > $2 OR (updated_at = $2 AND id > $3))) + (SELECT COUNT(*) FROM marcacoes WHERE usuario_id = $1 AND (updated_at > $2 OR (updated_at = $2 AND id > $3))) + (SELECT COUNT(*) FROM artigos WHERE usuario_id = $1 AND (updated_at > $2 OR (updated_at = $2 AND id > $3)))`, uid, last.updatedAt, last.id).Scan(&cnt)
		} else {
			_ = a.DB.DB().QueryRow(`SELECT (SELECT COUNT(*) FROM notas WHERE usuario_id = $1 AND (updated_at, 'nota'::text, id) > ($2, $3::text, $4)) + (SELECT COUNT(*) FROM marcacoes WHERE usuario_id = $1 AND (updated_at, 'marcacao'::text, id) > ($2, $3::text, $4)) + (SELECT COUNT(*) FROM artigos WHERE usuario_id = $1 AND (updated_at, 'artigo'::text, id) > ($2, $3::text, $4))`, uid, last.updatedAt, last.entity, last.id).Scan(&cnt)
		}
		hasMore = cnt > 0
	}
	changes := make([]syncChange, 0, len(rows))
	var lastTime time.Time
	var lastEntity string
	var lastID int64
	for _, r := range rows {
		changes = append(changes, syncChange{
			Entity:    r.entity,
			ID:        r.id,
			ClientID:  r.clientID,
			Version:   r.version,
			UpdatedAt: r.updatedAt.Format(time.RFC3339Nano),
			Deleted:   r.deleted,
			Data:      r.data,
		})
		lastTime = r.updatedAt
		lastEntity = r.entity
		lastID = r.id
	}
	nextCursor := ""
	if len(rows) > 0 {
		nextCursor = encodeCursor(lastTime, lastEntity, lastID)
	} else {
		if cursorTime.IsZero() && cursorEntity == "" && cursorID == 0 {
			nextCursor = ""
		} else {
			nextCursor = encodeCursor(cursorTime, cursorEntity, cursorID)
		}
	}
	return changes, nextCursor, hasMore, nil
}

func decodeTagsLocal(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return []string{}
	}
	if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
		raw = raw[1 : len(raw)-1]
	} else {
		return []string{}
	}
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var out []string
	var cur strings.Builder
	inQuote := false
	escaped := false
	for _, r := range raw {
		if escaped {
			cur.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inQuote = !inQuote
			continue
		}
		if r == ',' && !inQuote {
			s := strings.TrimSpace(cur.String())
			if s != "" {
				out = append(out, s)
			}
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	s := strings.TrimSpace(cur.String())
	if s != "" {
		out = append(out, s)
	}
	if out == nil {
		return []string{}
	}
	return out
}
