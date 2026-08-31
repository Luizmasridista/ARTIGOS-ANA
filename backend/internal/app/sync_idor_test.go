package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func syncDo(t *testing.T, tsURL string, token string, body any) syncResponse {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, tsURL+"/api/sync", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("sync request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync status %d", resp.StatusCode)
	}
	var out syncResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func pullDo(t *testing.T, tsURL string, token string) syncPullResponse {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, tsURL+"/api/sync/pull?limit=100", nil)
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pull status %d", resp.StatusCode)
	}
	var out syncPullResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func TestSyncIDOR_CreateNotaArtigoDeOutroUsuario(t *testing.T) {
	requireDB(t)
	ts, a := newTestServer(t)
	ensureTestUser(t, a, "Ana Bagatinii")
	bobID, bobTok := ensureTestUser(t, a, "SyncBob1")
	// Ana cria artigo
	criado := uploadPDF(t, ts, "Artigo IDOR Ana")
	artigoAna := int64(criado["id"].(float64))
	// Bob tenta criar nota no artigo de Ana via sync
	out := syncDo(t, ts.URL, bobTok, map[string]any{
		"deviceId": "dev-bob",
		"operations": []map[string]any{
			{"opId": "op-bob-1", "clientId": "cli-bob-1", "entity": "nota", "action": "create", "data": map[string]any{"artigo_id": artigoAna, "pagina": 1, "texto": "tentativa IDOR"}},
		},
	})
	if len(out.Results) != 1 || out.Results[0].Status != "error" {
		t.Fatalf("Bob criar nota em artigo de Ana deveria error, veio %+v", out.Results)
	}
	// verifica que nota não foi inserida no banco
	var cnt int
	_ = a.DB.DB().QueryRow(`SELECT COUNT(*) FROM notas WHERE usuario_id = $1`, bobID).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("nota IDOR foi inserida, count %d", cnt)
	}
	_ = artigoAna
}

func TestSyncIDOR_CreateMarcacaoArtigoDeOutroUsuario(t *testing.T) {
	requireDB(t)
	ts, a := newTestServer(t)
	_, anaTok := ensureTestUser(t, a, "Ana Bagatinii")
	_, bobTok := ensureTestUser(t, a, "SyncBob2")
	criado := uploadPDF(t, ts, "Artigo IDOR Marc Ana")
	artigoAna := int64(criado["id"].(float64))
	out := syncDo(t, ts.URL, bobTok, map[string]any{
		"deviceId": "dev-bob",
		"operations": []map[string]any{
			{"opId": "op-bob-m1", "clientId": "cli-bob-m1", "entity": "marcacao", "action": "create", "data": map[string]any{"artigo_id": artigoAna, "pagina": 1, "tipo": "highlight", "cor": "#FFEB3B", "palavras": [][]float64{{1, 2, 3, 4}}, "texto": "x"}},
		},
	})
	if out.Results[0].Status != "error" {
		t.Fatalf("Bob marcacao em artigo de Ana deveria error, veio %+v", out.Results[0])
	}
	// também testa artigoId camelCase no data
	out2 := syncDo(t, ts.URL, bobTok, map[string]any{
		"deviceId": "dev-bob",
		"operations": []map[string]any{
			{"opId": "op-bob-m2", "clientId": "cli-bob-m2", "entity": "marcacao", "action": "create", "data": map[string]any{"artigoId": artigoAna, "pagina": 1, "tipo": "highlight", "cor": "#FFEB3B", "palavras": [][]float64{{1, 2, 3, 4}}, "texto": "x"}},
		},
	})
	if out2.Results[0].Status != "error" {
		t.Fatalf("artigoId camel deveria também dar error, veio %+v", out2.Results[0])
	}
	// operação nível artigoId
	out3 := syncDo(t, ts.URL, bobTok, map[string]any{
		"deviceId": "dev-bob",
		"operations": []map[string]any{
			{"opId": "op-bob-m3", "clientId": "cli-bob-m3", "entity": "marcacao", "action": "create", "artigoId": artigoAna, "data": map[string]any{"pagina": 1, "tipo": "highlight", "cor": "#FFEB3B", "palavras": [][]float64{{1, 2, 3, 4}}, "texto": "x"}},
		},
	})
	if out3.Results[0].Status != "error" {
		t.Fatalf("artigoId no nível da operação deveria error, veio %+v", out3.Results[0])
	}
	_ = anaTok
}

func TestSyncIDOR_ArtigoExcluido(t *testing.T) {
	requireDB(t)
	ts, a := newTestServer(t)
	_, anaTok := ensureTestUser(t, a, "Ana Bagatinii")
	criado := uploadPDF(t, ts, "Artigo Sera Excluido")
	artigoID := int64(criado["id"].(float64))
	// soft-delete artigo (set deleted_at)
	_, _ = a.DB.DB().Exec(`UPDATE artigos SET deleted_at = now(), updated_at = now() WHERE id = $1`, artigoID)
	out := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-a",
		"operations": []map[string]any{
			{"opId": "op-del-artigo", "clientId": "cli-del-1", "entity": "nota", "action": "create", "data": map[string]any{"artigo_id": artigoID, "pagina": 1, "texto": "nota em artigo excluido"}},
		},
	})
	if out.Results[0].Status != "error" {
		t.Fatalf("nota em artigo excluído deveria error, veio %+v", out.Results[0])
	}
	out2 := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-a",
		"operations": []map[string]any{
			{"opId": "op-del-artigo-m", "clientId": "cli-del-m1", "entity": "marcacao", "action": "create", "data": map[string]any{"artigo_id": artigoID, "pagina": 1, "tipo": "highlight", "cor": "#FFEB3B", "palavras": [][]float64{{1, 2, 3, 4}}, "texto": "x"}},
		},
	})
	if out2.Results[0].Status != "error" {
		t.Fatalf("marcacao em artigo excluído deveria error, veio %+v", out2.Results[0])
	}
}

func TestSyncIDOR_NotaMarcacaoIDValidacao(t *testing.T) {
	requireDB(t)
	ts, a := newTestServer(t)
	_, anaTok := ensureTestUser(t, a, "Ana Bagatinii")
	bobID, bobTok := ensureTestUser(t, a, "SyncBob3")
	// cria artigos para ambos
	criadoAna := uploadPDF(t, ts, "Artigo Ana Marcacao")
	artigoAna := int64(criadoAna["id"].(float64))
	bobArtigoID, _, _ := a.DB.CreateArtigoForUser("Artigo Bob Marcacao", "pdfs/bob3.pdf", bobID)
	// Ana cria marcacao via sync no seu artigo
	outM := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-ana",
		"operations": []map[string]any{
			{"opId": "op-ana-m1", "clientId": "cli-ana-m1", "entity": "marcacao", "action": "create", "data": map[string]any{"artigo_id": artigoAna, "pagina": 1, "tipo": "highlight", "cor": "#FFEB3B", "palavras": [][]float64{{1, 2, 3, 4}}, "texto": "trecho ana"}},
		},
	})
	if outM.Results[0].Status != "applied" {
		t.Fatalf("Ana marcacao create deveria applied, veio %+v", outM.Results[0])
	}
	marcID := *outM.Results[0].ID
	// Bob tenta criar nota no seu artigo vinculando marcacao de Ana (cross user)
	outBob := syncDo(t, ts.URL, bobTok, map[string]any{
		"deviceId": "dev-bob",
		"operations": []map[string]any{
			{"opId": "op-bob-nota-marc", "clientId": "cli-bob-nota-marc", "entity": "nota", "action": "create", "data": map[string]any{"artigo_id": bobArtigoID, "pagina": 1, "texto": "nota bob com marcacao de ana", "marcacao_id": marcID}},
		},
	})
	if outBob.Results[0].Status != "error" {
		t.Fatalf("nota com marcacao de outro usuário deveria error, veio %+v", outBob.Results[0])
	}
	// Bob cria nota sem marcacao no seu artigo (ok)
	outBob2 := syncDo(t, ts.URL, bobTok, map[string]any{
		"deviceId": "dev-bob",
		"operations": []map[string]any{
			{"opId": "op-bob-nota-ok", "clientId": "cli-bob-nota-ok", "entity": "nota", "action": "create", "data": map[string]any{"artigo_id": bobArtigoID, "pagina": 1, "texto": "nota ok"}},
		},
	})
	if outBob2.Results[0].Status != "applied" {
		t.Fatalf("nota bob ok deveria applied, veio %+v", outBob2.Results[0])
	}
	// Ana cria nota válida vinculada à sua marcacao (mesmo artigo)
	outAnaNota := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-ana",
		"operations": []map[string]any{
			{"opId": "op-ana-nota-marc", "clientId": "cli-ana-nota-marc", "entity": "nota", "action": "create", "data": map[string]any{"artigo_id": artigoAna, "pagina": 1, "texto": "nota ana vinculada", "marcacao_id": marcID}},
		},
	})
	if outAnaNota.Results[0].Status != "applied" {
		t.Fatalf("nota ana vinculada deveria applied, veio %+v", outAnaNota.Results[0])
	}
	// cria segundo artigo para Ana e tenta vincular marcacao de outro artigo mesmo usuário (cross artigo)
	criadoAna2 := uploadPDF(t, ts, "Artigo Ana 2")
	artigoAna2 := int64(criadoAna2["id"].(float64))
	outCrossArtigo := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-ana",
		"operations": []map[string]any{
			{"opId": "op-ana-cross-artigo", "clientId": "cli-ana-cross", "entity": "nota", "action": "create", "data": map[string]any{"artigo_id": artigoAna2, "pagina": 1, "texto": "nota cross", "marcacao_id": marcID}},
		},
	})
	if outCrossArtigo.Results[0].Status != "error" {
		t.Fatalf("marcacao_id cross artigo mesmo usuario deveria error, veio %+v", outCrossArtigo.Results[0])
	}
	// marcacao excluída: soft delete marcacao de Ana e tenta vincular nova nota
	_, _ = a.DB.DB().Exec(`UPDATE marcacoes SET deleted_at = now(), updated_at = now() WHERE id = $1`, marcID)
	outDeleted := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-ana",
		"operations": []map[string]any{
			{"opId": "op-ana-marc-del", "clientId": "cli-ana-del", "entity": "nota", "action": "create", "data": map[string]any{"artigo_id": artigoAna, "pagina": 1, "texto": "nota marc deletada", "marcacao_id": marcID}},
		},
	})
	if outDeleted.Results[0].Status != "error" {
		t.Fatalf("marcacao deletada deveria error, veio %+v", outDeleted.Results[0])
	}
}

func TestSyncIDOR_UpdateNotaMarcacaoIDValidation(t *testing.T) {
	requireDB(t)
	ts, a := newTestServer(t)
	_, anaTok := ensureTestUser(t, a, "Ana Bagatinii")
	bobID, _ := ensureTestUser(t, a, "SyncBob4")
	criadoAna := uploadPDF(t, ts, "Artigo Ana Update")
	artigoAna := int64(criadoAna["id"].(float64))
	bobArtigoID, _, _ := a.DB.CreateArtigoForUser("Artigo Bob Update", "pdfs/bob4.pdf", bobID)
	// Ana cria nota sem marcacao
	outNota := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-ana",
		"operations": []map[string]any{
			{"opId": "op-ana-nota-upd", "clientId": "cli-ana-nota-upd", "entity": "nota", "action": "create", "data": map[string]any{"artigo_id": artigoAna, "pagina": 1, "texto": "nota para update"}},
		},
	})
	if outNota.Results[0].Status != "applied" {
		t.Fatalf("create nota ana update deveria applied, veio %+v", outNota.Results[0])
	}
	notaClient := "cli-ana-nota-upd"
	// cria marcacao no artigo de Bob via store direto
	mBob, _ := a.DB.CreateMarcacaoForUser(bobArtigoID, bobID, 1, "highlight", "#FFEB3B", []byte("[]"), "trecho bob")
	bobMarcID := mBob.ID

	// tenta update da nota de Ana vinculando marcacao de Bob
	outUpd := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-ana",
		"operations": []map[string]any{
			{"opId": "op-ana-update-marc", "clientId": notaClient, "entity": "nota", "action": "update", "data": map[string]any{"marcacao_id": bobMarcID}},
		},
	})
	if outUpd.Results[0].Status != "error" {
		t.Fatalf("update com marcacao de outro usuário deveria error, veio %+v", outUpd.Results[0])
	}
}

func TestSyncPullIncluiArtigoID(t *testing.T) {
	requireDB(t)
	ts, a := newTestServer(t)
	_, anaTok := ensureTestUser(t, a, "Ana Bagatinii")
	criado := uploadPDF(t, ts, "Artigo Pull ArtigoID")
	artigoID := int64(criado["id"].(float64))
	// cria via sync nota e marcacao
	outNota := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-ana",
		"operations": []map[string]any{
			{"opId": "op-pull-nota", "clientId": "cli-pull-nota", "entity": "nota", "action": "create", "data": map[string]any{"artigo_id": artigoID, "pagina": 1, "texto": "pull nota"}},
		},
	})
	if outNota.Results[0].Status != "applied" {
		t.Fatalf("create pull nota %v", outNota.Results[0])
	}
	outMarc := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-ana",
		"operations": []map[string]any{
			{"opId": "op-pull-marc", "clientId": "cli-pull-marc", "entity": "marcacao", "action": "create", "data": map[string]any{"artigo_id": artigoID, "pagina": 1, "tipo": "highlight", "cor": "#FFEB3B", "palavras": [][]float64{{1, 2, 3, 4}}, "texto": "pull marc"}},
		},
	})
	if outMarc.Results[0].Status != "applied" {
		t.Fatalf("create pull marc %v", outMarc.Results[0])
	}
	// também cria nota com artigoId camelCase no data
	outNotaCamel := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-ana",
		"operations": []map[string]any{
			{"opId": "op-pull-nota-camel", "clientId": "cli-pull-nota-camel", "entity": "nota", "action": "create", "data": map[string]any{"artigoId": artigoID, "pagina": 1, "texto": "pull camel"}},
		},
	})
	if outNotaCamel.Results[0].Status != "applied" {
		t.Fatalf("create pull nota camel deveria applied, veio %v", outNotaCamel.Results[0])
	}
	// nota com artigoId no nível da operação
	outNotaOp := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-ana",
		"operations": []map[string]any{
			{"opId": "op-pull-nota-op", "clientId": "cli-pull-nota-op", "entity": "nota", "action": "create", "artigoId": artigoID, "data": map[string]any{"pagina": 1, "texto": "pull op"}},
		},
	})
	if outNotaOp.Results[0].Status != "applied" {
		t.Fatalf("create pull nota op-level artigoId deveria applied, veio %v", outNotaOp.Results[0])
	}
	pull := pullDo(t, ts.URL, anaTok)
	foundNota := false
	foundMarc := false
	for _, ch := range pull.Changes {
		var d map[string]any
		_ = json.Unmarshal(ch.Data, &d)
		if ch.Entity == "nota" && (ch.ClientID == "cli-pull-nota" || ch.ClientID == "cli-pull-nota-camel" || ch.ClientID == "cli-pull-nota-op") {
			if v, ok := d["artigo_id"]; !ok {
				t.Fatalf("nota pull sem artigo_id data=%s", string(ch.Data))
			} else if int64(v.(float64)) != artigoID {
				t.Fatalf("nota artigo_id divergente esperado %d veio %v", artigoID, v)
			}
			foundNota = true
		}
		if ch.Entity == "marcacao" && ch.ClientID == "cli-pull-marc" {
			if v, ok := d["artigo_id"]; !ok {
				t.Fatalf("marcacao pull sem artigo_id data=%s", string(ch.Data))
			} else if int64(v.(float64)) != artigoID {
				t.Fatalf("marcacao artigo_id divergente esperado %d veio %v", artigoID, v)
			}
			foundMarc = true
		}
	}
	if !foundNota {
		t.Fatalf("nota não encontrada no pull")
	}
	if !foundMarc {
		t.Fatalf("marcacao não encontrada no pull")
	}
	// tombstone também deve trazer artigo_id
	// deleta nota
	outDel := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-ana",
		"operations": []map[string]any{
			{"opId": "op-pull-del", "clientId": "cli-pull-nota", "entity": "nota", "action": "delete"},
		},
	})
	if outDel.Results[0].Status != "applied" {
		t.Fatalf("delete para tombstone %v", outDel.Results[0])
	}
	pull2 := pullDo(t, ts.URL, anaTok)
	foundTomb := false
	for _, ch := range pull2.Changes {
		if ch.Entity == "nota" && ch.ClientID == "cli-pull-nota" && ch.Deleted {
			var d map[string]any
			_ = json.Unmarshal(ch.Data, &d)
			if _, ok := d["artigo_id"]; !ok {
				t.Fatalf("tombstone nota sem artigo_id")
			}
			foundTomb = true
		}
	}
	if !foundTomb {
		t.Fatalf("tombstone nota com artigo_id não encontrado no pull")
	}
	_ = a
}

func TestSyncIdempotenciaPreservada(t *testing.T) {
	requireDB(t)
	ts, a := newTestServer(t)
	_, anaTok := ensureTestUser(t, a, "Ana Bagatinii")
	criado := uploadPDF(t, ts, "Artigo Idemp")
	artigoID := int64(criado["id"].(float64))
	out1 := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-ana",
		"operations": []map[string]any{
			{"opId": "op-idemp", "clientId": "cli-idemp", "entity": "nota", "action": "create", "data": map[string]any{"artigo_id": artigoID, "pagina": 1, "texto": "idemp"}},
		},
	})
	if out1.Results[0].Status != "applied" {
		t.Fatalf("primeiro applied esperado, veio %v", out1.Results[0])
	}
	out2 := syncDo(t, ts.URL, anaTok, map[string]any{
		"deviceId": "dev-ana",
		"operations": []map[string]any{
			{"opId": "op-idemp", "clientId": "cli-idemp", "entity": "nota", "action": "create", "data": map[string]any{"artigo_id": artigoID, "pagina": 1, "texto": "idemp"}},
		},
	})
	if out2.Results[0].Status != "already_applied" {
		t.Fatalf("idempotência deveria already_applied, veio %v", out2.Results[0])
	}
	_ = a
}
