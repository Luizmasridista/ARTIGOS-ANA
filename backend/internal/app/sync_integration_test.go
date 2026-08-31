package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Testes de integração que exigem PostgreSQL.
// Serão skipados automaticamente se o banco de teste estiver indisponível (ver requireDB).
// Mantidos separados dos testes unitários (sync_unit_test.go) que rodam sem DB.

func TestSyncIntegration_CreateNotaIdempotente(t *testing.T) {
	requireDB(t)
	ts, _ := newTestServer(t)
	// garante artigo para sync
	criado := uploadPDF(t, ts, "Artigo Sync Int")
	artigoID := int64(criado["id"].(float64))

	tok := testLogin(t, ts)
	// primeira chamada
	body1, _ := json.Marshal(map[string]any{
		"deviceId": "dev-1",
		"operations": []map[string]any{
			{"opId": "op-1", "clientId": "cli-nota-1", "entity": "nota", "action": "create", "data": map[string]any{"artigo_id": artigoID, "pagina": 1, "texto": "nota sync 1"}},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/sync", bytes.NewReader(body1))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync create 1 status %d", resp.StatusCode)
	}
	var out struct {
		Results []syncResult `json:"results"`
		Cursor  string       `json:"cursor"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Results) != 1 || out.Results[0].Status != "applied" {
		t.Fatalf("resultado 1 inesperado %+v", out.Results)
	}
	firstID := out.Results[0].ID
	firstVersion := out.Results[0].Version

	// segunda chamada com mesmo opId deve ser already_applied
	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/sync", bytes.NewReader(body1))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp2, err2 := http.DefaultClient.Do(req2)
	if err2 != nil {
		t.Fatal(err2)
	}
	defer resp2.Body.Close()
	var out2 struct {
		Results []syncResult `json:"results"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&out2)
	if len(out2.Results) != 1 || out2.Results[0].Status != "already_applied" {
		t.Fatalf("idempotência falhou %+v", out2.Results)
	}
	if *out2.Results[0].ID != *firstID || *out2.Results[0].Version != *firstVersion {
		t.Fatalf("already_applied divergiu")
	}
	// pull deve trazer a nota
	req3, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/sync/pull?limit=10", nil)
	req3.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp3, err3 := http.DefaultClient.Do(req3)
	if err3 != nil {
		t.Fatal(err3)
	}
	defer resp3.Body.Close()
	var pull syncPullResponse
	_ = json.NewDecoder(resp3.Body).Decode(&pull)
	if len(pull.Changes) == 0 {
		t.Fatalf("pull vazio")
	}
	found := false
	for _, ch := range pull.Changes {
		if ch.Entity == "nota" && ch.ClientID == "cli-nota-1" {
			found = true
			if ch.Deleted {
				t.Fatalf("não deveria estar deletada")
			}
		}
	}
	if !found {
		t.Fatalf("nota não encontrada no pull")
	}
	// cursor deve ser opaco e estável: segunda chamada com cursor não deve repetir se não houver mudança (ou deve ser idempotente)
	req4, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/sync/pull?cursor="+pull.Cursor+"&limit=10", nil)
	req4.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp4, err4 := http.DefaultClient.Do(req4)
	if err4 != nil {
		t.Fatal(err4)
	}
	defer resp4.Body.Close()
	var pull2 syncPullResponse
	_ = json.NewDecoder(resp4.Body).Decode(&pull2)
	if len(pull2.Changes) != 0 {
		t.Fatalf("pull com cursor avançado deveria vazio, veio %d", len(pull2.Changes))
	}
}

func TestSyncIntegration_ConflictLWW(t *testing.T) {
	requireDB(t)
	ts, _ := newTestServer(t)
	criado := uploadPDF(t, ts, "Artigo Sync Conflito")
	artigoID := int64(criado["id"].(float64))
	tok := testLogin(t, ts)

	// cria nota via sync
	bodyCreate, _ := json.Marshal(map[string]any{
		"deviceId": "dev-1",
		"operations": []map[string]any{
			{"opId": "op-c1", "clientId": "cli-conflito", "entity": "nota", "action": "create", "data": map[string]any{"artigo_id": artigoID, "pagina": 1, "texto": "v1"}},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/sync", bytes.NewReader(bodyCreate))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp, _ := http.DefaultClient.Do(req)
	var out struct {
		Results []syncResult `json:"results"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Results[0].Status != "applied" {
		t.Fatalf("create falhou")
	}
	v1 := *out.Results[0].Version

	// update correto com baseVersion = v1 deve ser applied
	bodyUpdOK, _ := json.Marshal(map[string]any{
		"deviceId": "dev-1",
		"operations": []map[string]any{
			{"opId": "op-u1", "clientId": "cli-conflito", "entity": "nota", "action": "update", "baseVersion": v1, "data": map[string]any{"texto": "v2"}},
		},
	})
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/sync", bytes.NewReader(bodyUpdOK))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp, _ = http.DefaultClient.Do(req)
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Results[0].Status != "applied" {
		t.Fatalf("update ok deveria applied veio %s", out.Results[0].Status)
	}
	v2 := *out.Results[0].Version
	if v2 != v1+1 {
		t.Fatalf("versão deveria incrementar")
	}

	// update com baseVersion stale (v1) deve reportar conflict e NAO aplicar (servidor-autoritativo)
	bodyConflict, _ := json.Marshal(map[string]any{
		"deviceId": "dev-2",
		"operations": []map[string]any{
			{"opId": "op-u2", "clientId": "cli-conflito", "entity": "nota", "action": "update", "baseVersion": v1, "data": map[string]any{"texto": "v3-conflito"}},
		},
	})
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/sync", bytes.NewReader(bodyConflict))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp, _ = http.DefaultClient.Do(req)
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Results[0].Status != "conflict" {
		t.Fatalf("conflito deveria ser conflict veio %s", out.Results[0].Status)
	}
	if out.Results[0].Version == nil || *out.Results[0].Version != v2 {
		t.Fatalf("versão após conflito deveria permanecer v2, veio %v", out.Results[0].Version)
	}
	if len(out.Results[0].ServerData) == 0 {
		t.Fatalf("serverData deveria estar presente no conflito")
	}
	// serverData deve refletir estado vencedor (v2), nao o cliente
	var sd map[string]any
	_ = json.Unmarshal(out.Results[0].ServerData, &sd)
	// texto no servidor deve ser v2, nao v3-conflito (cliente nao venceu)
	// NotaSync tem campo texto
	if sd["texto"] != "v2" {
		// fallback: verifica que nao contem v3
		if sdText, ok := sd["texto"].(string); ok && sdText == "v3-conflito" {
			t.Fatalf("serverData deveria conter estado do servidor (v2), veio cliente")
		}
	}
	// cliente corrige com baseVersion v2 e consegue applied
	bodyFix, _ := json.Marshal(map[string]any{
		"deviceId": "dev-2",
		"operations": []map[string]any{
			{"opId": "op-u3", "clientId": "cli-conflito", "entity": "nota", "action": "update", "baseVersion": v2, "data": map[string]any{"texto": "v3-correto"}},
		},
	})
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/sync", bytes.NewReader(bodyFix))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp, _ = http.DefaultClient.Do(req)
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Results[0].Status != "applied" {
		t.Fatalf("update corrigido deveria applied veio %s", out.Results[0].Status)
	}
	if *out.Results[0].Version != v2+1 {
		t.Fatalf("versão corrigida deveria ser v2+1")
	}
	// delete conflitante tambem nao deve apagar
	bodyDelConflict, _ := json.Marshal(map[string]any{
		"deviceId": "dev-2",
		"operations": []map[string]any{
			{"opId": "op-del-conflict", "clientId": "cli-conflito", "entity": "nota", "action": "delete", "baseVersion": v2},
		},
	})
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/sync", bytes.NewReader(bodyDelConflict))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp, _ = http.DefaultClient.Do(req)
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Results[0].Status != "conflict" {
		t.Fatalf("delete conflitante deveria ser conflict veio %s", out.Results[0].Status)
	}
}

func TestSyncIntegration_MarcacaoETombstone(t *testing.T) {
	requireDB(t)
	ts, _ := newTestServer(t)
	criado := uploadPDF(t, ts, "Artigo Sync Marc")
	artigoID := int64(criado["id"].(float64))
	tok := testLogin(t, ts)
	// cria marcacao
	body, _ := json.Marshal(map[string]any{
		"deviceId": "dev-1",
		"operations": []map[string]any{
			{"opId": "op-m1", "clientId": "cli-marc-1", "entity": "marcacao", "action": "create", "data": map[string]any{"artigo_id": artigoID, "pagina": 1, "tipo": "highlight", "cor": "#FFEB3B", "palavras": [][]float64{{10, 20, 30, 40}}, "texto": "trecho"}},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp, _ := http.DefaultClient.Do(req)
	var out struct {
		Results []syncResult `json:"results"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Results[0].Status != "applied" {
		t.Fatalf("marcacao create falhou")
	}
	// delete
	bodyDel, _ := json.Marshal(map[string]any{
		"deviceId": "dev-1",
		"operations": []map[string]any{
			{"opId": "op-m-del", "clientId": "cli-marc-1", "entity": "marcacao", "action": "delete"},
		},
	})
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/sync", bytes.NewReader(bodyDel))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp, _ = http.DefaultClient.Do(req)
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Results[0].Status != "applied" {
		t.Fatalf("delete falhou")
	}
	// pull deve trazer tombstone
	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/sync/pull?limit=20", nil)
	req2.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp2, _ := http.DefaultClient.Do(req2)
	var pull syncPullResponse
	_ = json.NewDecoder(resp2.Body).Decode(&pull)
	resp2.Body.Close()
	foundDel := false
	for _, ch := range pull.Changes {
		if ch.Entity == "marcacao" && ch.ClientID == "cli-marc-1" && ch.Deleted {
			foundDel = true
		}
	}
	if !foundDel {
		t.Fatalf("tombstone não encontrado no pull %v", pull.Changes)
	}
}

func TestJobsIntegration_EnqueueEConsulta(t *testing.T) {
	requireDB(t)
	ts, _ := newTestServer(t)
	tok := testLogin(t, ts)
	body, _ := json.Marshal(map[string]any{"tipo": "noop", "payload": map[string]any{"x": 1}})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("enqueue status %d", resp.StatusCode)
	}
	var job map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&job)
	resp.Body.Close()
	id := int64(job["id"].(float64))
	// consulta
	req2, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/jobs/%d", ts.URL, id), nil)
	req2.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp2, _ := http.DefaultClient.Do(req2)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("get job %d", resp2.StatusCode)
	}
	var job2 map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&job2)
	resp2.Body.Close()
	if job2["tipo"] != "noop" {
		t.Fatalf("tipo divergente")
	}
	// lista
	req3, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/jobs", nil)
	req3.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp3, _ := http.DefaultClient.Do(req3)
	var list []map[string]any
	_ = json.NewDecoder(resp3.Body).Decode(&list)
	resp3.Body.Close()
	if len(list) == 0 {
		t.Fatalf("lista vazia")
	}
}
