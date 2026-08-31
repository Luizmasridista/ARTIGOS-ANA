package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// cria usuario de teste diretamente no banco e devolve cookie JWT
func ensureTestUser(t *testing.T, a *App, nome string) (int64, string) {
	t.Helper()
	// tenta pegar existente
	u, err := a.DB.GetUsuarioByNome(nome)
	if err != nil {
		t.Fatalf("GetUsuarioByNome: %v", err)
	}
	var uid int64
	if u != nil {
		uid = u.ID
	} else {
		// insere com senha vazia
		_, err = a.DB.DB().Exec(`INSERT INTO usuarios (nome, senha_hash, criado_em) VALUES ($1,'', now()) ON CONFLICT (nome) DO NOTHING`, nome)
		if err != nil {
			t.Fatalf("insert usuario %s: %v", nome, err)
		}
		u2, err := a.DB.GetUsuarioByNome(nome)
		if err != nil || u2 == nil {
			t.Fatalf("re-fetch usuario %s: %v", nome, err)
		}
		uid = u2.ID
	}
	token, err := issueJWT(a.JWTSecret, uid, nome)
	if err != nil {
		t.Fatalf("issueJWT: %v", err)
	}
	return uid, token
}

func doAsUser(t *testing.T, method, url string, body io.Reader, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		// content-type será setado pelo chamador se necessário; default json
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "ana_session", Value: token})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func TestIDOR_ArtigoIsolamento(t *testing.T) {
	ts, a := newTestServer(t)
	anaID, anaToken := ensureTestUser(t, a, "Ana Bagatinii")
	_ = anaID
	bobID, bobToken := ensureTestUser(t, a, "UsuarioIDOR")
	_ = bobID

	// Ana cria artigo
	criado := uploadPDF(t, ts, "Artigo Ana IDOR")
	artigoID := int64(criado["id"].(float64))

	// Bob não deve listar o artigo de Ana
	resp := doAsUser(t, http.MethodGet, ts.URL+"/api/artigos", nil, bobToken)
	var listBob []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&listBob)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Bob list status %d", resp.StatusCode)
	}
	for _, it := range listBob {
		if int64(it["id"].(float64)) == artigoID {
			t.Fatalf("Bob listou artigo de Ana: %v", it)
		}
	}
	if len(listBob) != 0 {
		t.Fatalf("Bob deveria ver 0 artigos, viu %d: %v", len(listBob), listBob)
	}

	// Bob não deve conseguir GET do artigo
	resp = doAsUser(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d", ts.URL, artigoID), nil, bobToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob GET artigo de Ana deveria 404, veio %d", resp.StatusCode)
	}

	// Bob não deve ver paginas
	resp = doAsUser(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/paginas/1/imagem", ts.URL, artigoID), nil, bobToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob imagem deveria 404 veio %d", resp.StatusCode)
	}
	resp = doAsUser(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/paginas/1/camada", ts.URL, artigoID), nil, bobToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob camada deveria 404 veio %d", resp.StatusCode)
	}

	// Bob não deve conseguir criar marcação no artigo de Ana (IDOR via artigo_id)
	body, _ := json.Marshal(map[string]any{"pagina": 1, "tipo": "highlight", "cor": "#FFEB3B", "palavras": [][]float64{{10, 10, 20, 20}}, "texto": "tentativa"})
	resp = doAsUser(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/marcacoes", ts.URL, artigoID), bytes.NewReader(body), bobToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob create marcacao em artigo de Ana deveria 404 veio %d", resp.StatusCode)
	}

	// Ana ainda deve listar e GET seu artigo normalmente
	resp = doAsUser(t, http.MethodGet, ts.URL+"/api/artigos", nil, anaToken)
	var listAna []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&listAna)
	resp.Body.Close()
	if len(listAna) != 1 || int64(listAna[0]["id"].(float64)) != artigoID {
		t.Fatalf("Ana deveria ver seu artigo, veio %v", listAna)
	}
	resp = doAsUser(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d", ts.URL, artigoID), nil, anaToken)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Ana GET deveria 200 veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Bob não deve deletar artigo de Ana
	resp = doAsUser(t, http.MethodDelete, fmt.Sprintf("%s/api/artigos/%d", ts.URL, artigoID), nil, bobToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob DELETE deveria 404 veio %d", resp.StatusCode)
	}
	// Ana ainda vê
	resp = doAsUser(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d", ts.URL, artigoID), nil, anaToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Ana artigo deveria ainda existir após tentativa Bob DELETE, status %d", resp.StatusCode)
	}

	// Bob cria seu próprio artigo; Ana não deve ver
	// usa upload como Bob: precisamos fazer multipart com token Bob
	// para simplificar, testa via store direto: CreateArtigoForUser com bobID
	bobArtigoID, _, err := a.DB.CreateArtigoForUser("Artigo Bob", "pdfs/bob.pdf", bobID)
	if err != nil {
		t.Fatalf("CreateArtigoForUser Bob: %v", err)
	}
	// Ana lista não deve conter Bob
	resp = doAsUser(t, http.MethodGet, ts.URL+"/api/artigos", nil, anaToken)
	_ = json.NewDecoder(resp.Body).Decode(&listAna)
	resp.Body.Close()
	for _, it := range listAna {
		if int64(it["id"].(float64)) == bobArtigoID {
			t.Fatalf("Ana listou artigo de Bob")
		}
	}
	// Bob lista deve conter apenas dele
	resp = doAsUser(t, http.MethodGet, ts.URL+"/api/artigos", nil, bobToken)
	_ = json.NewDecoder(resp.Body).Decode(&listBob)
	resp.Body.Close()
	if len(listBob) != 1 || int64(listBob[0]["id"].(float64)) != bobArtigoID {
		t.Fatalf("Bob deveria ver apenas seu artigo, veio %v", listBob)
	}
	// Ana não deve acessar Bob
	resp = doAsUser(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d", ts.URL, bobArtigoID), nil, anaToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Ana GET Bob artigo deveria 404 veio %d", resp.StatusCode)
	}
}

func TestIDOR_MarcacoesNotasIsoladas(t *testing.T) {
	ts, a := newTestServer(t)
	_, anaToken := ensureTestUser(t, a, "Ana Bagatinii")
	bobID, bobToken := ensureTestUser(t, a, "UsuarioIDOR2")
	// cria DB de teste limpo
	criado := uploadPDF(t, ts, "Artigo Notas IDOR")
	artigoID := int64(criado["id"].(float64))

	// Ana cria marcacao e nota
	body, _ := json.Marshal(map[string]any{"pagina": 1, "tipo": "highlight", "cor": "#FFEB3B", "palavras": [][]float64{{10, 10, 20, 20}}, "texto": "trecho Ana"})
	resp := doAsUser(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/marcacoes", ts.URL, artigoID), bytes.NewReader(body), anaToken)
	var marc map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&marc)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Ana create marcacao %d", resp.StatusCode)
	}
	marcID := int64(marc["id"].(float64))

	body, _ = json.Marshal(map[string]any{"pagina": 1, "texto": "nota Ana"})
	resp = doAsUser(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/notas", ts.URL, artigoID), bytes.NewReader(body), anaToken)
	var nota map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&nota)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Ana create nota %d", resp.StatusCode)
	}
	notaID := int64(nota["id"].(float64))

	// Bob tenta listar marcacoes/notas do artigo de Ana => 404
	resp = doAsUser(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/marcacoes", ts.URL, artigoID), nil, bobToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob list marcacoes deveria 404 veio %d", resp.StatusCode)
	}
	resp = doAsUser(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/notas", ts.URL, artigoID), nil, bobToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob list notas deveria 404 veio %d", resp.StatusCode)
	}
	// Bob tenta update/delete usando IDs de Ana (IDOR direto)
	body, _ = json.Marshal(map[string]string{"cor": "#000000"})
	resp = doAsUser(t, http.MethodPatch, fmt.Sprintf("%s/api/artigos/%d/marcacoes/%d", ts.URL, artigoID, marcID), bytes.NewReader(body), bobToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob patch marcacao deveria 404 veio %d", resp.StatusCode)
	}
	resp = doAsUser(t, http.MethodDelete, fmt.Sprintf("%s/api/artigos/%d/marcacoes/%d", ts.URL, artigoID, marcID), nil, bobToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob delete marcacao deveria 404 veio %d", resp.StatusCode)
	}
	body, _ = json.Marshal(map[string]any{"texto": "hacked"})
	resp = doAsUser(t, http.MethodPatch, fmt.Sprintf("%s/api/artigos/%d/notas/%d", ts.URL, artigoID, notaID), bytes.NewReader(body), bobToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob patch nota deveria 404 veio %d", resp.StatusCode)
	}
	resp = doAsUser(t, http.MethodDelete, fmt.Sprintf("%s/api/artigos/%d/notas/%d", ts.URL, artigoID, notaID), nil, bobToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob delete nota deveria 404 veio %d", resp.StatusCode)
	}

	// Bob tenta criar nota usando marcacao_id de Ana mas em seu próprio artigo isolado
	bobArtigoID, _, err := a.DB.CreateArtigoForUser("Bob Artigo Notas", "pdfs/bob2.pdf", bobID)
	if err != nil {
		t.Fatalf("CreateArtigoForUser bob: %v", err)
	}
	// Bob cria nota normal no seu artigo (deve ok)
	body, _ = json.Marshal(map[string]any{"pagina": 1, "texto": "nota bob"})
	resp = doAsUser(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/notas", ts.URL, bobArtigoID), bytes.NewReader(body), bobToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("Bob create nota em seu artigo deveria 201 veio %d %s", resp.StatusCode, raw)
	}
	// Ana ainda vê suas
	resp = doAsUser(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/marcacoes", ts.URL, artigoID), nil, anaToken)
	var lista []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&lista)
	resp.Body.Close()
	if len(lista) != 1 {
		t.Fatalf("Ana deveria ver 1 marcacao, veio %d", len(lista))
	}
}

func TestIDOR_BuscaHistoricoSumarioExportCitacoesExcluidos(t *testing.T) {
	ts, a := newTestServer(t)
	_, anaToken := ensureTestUser(t, a, "Ana Bagatinii")
	_, bobToken := ensureTestUser(t, a, "UsuarioIDOR3")
	criado := uploadPDF(t, ts, "Artigo Protegidos")
	artigoID := int64(criado["id"].(float64))

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, fmt.Sprintf("/api/artigos/%d/busca?q=teste", artigoID)},
		{http.MethodGet, fmt.Sprintf("/api/artigos/%d/sumario", artigoID)},
		{http.MethodGet, fmt.Sprintf("/api/artigos/%d/historico", artigoID)},
		{http.MethodGet, fmt.Sprintf("/api/artigos/%d/citacoes", artigoID)},
		{http.MethodPost, fmt.Sprintf("/api/artigos/%d/varrer-citacoes", artigoID)},
		{http.MethodPost, fmt.Sprintf("/api/artigos/%d/exportar", artigoID)},
		{http.MethodGet, fmt.Sprintf("/api/artigos/%d/exportar", artigoID)},
	}
	for _, ep := range endpoints {
		resp := doAsUser(t, ep.method, ts.URL+ep.path, nil, bobToken)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("Bob %s %s deveria 404 veio %d", ep.method, ep.path, resp.StatusCode)
		}
		// Ana deve conseguir
		resp = doAsUser(t, ep.method, ts.URL+ep.path, nil, anaToken)
		// busca com q=teste pode retornar 200 mesmo vazio; sumario/historico etc 200; varrer-citacoes 200; exportar 200
		// para enriquecer citacao precisa id, skip
		if resp.StatusCode == http.StatusNotFound {
			t.Fatalf("Ana %s %s não deveria 404, veio 404", ep.method, ep.path)
		}
		resp.Body.Close()
	}

	// excluir-lote isolado: Bob tenta excluir artigo de Ana via lote, deve ser ignorado (204 mas sem efeito)
	body, _ := json.Marshal(map[string]any{"ids": []int64{artigoID}})
	resp := doAsUser(t, http.MethodPost, ts.URL+"/api/artigos/excluir-lote", bytes.NewReader(body), bobToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("Bob excluir-lote deveria 204 veio %d", resp.StatusCode)
	}
	// artigo ainda existe para Ana
	resp = doAsUser(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d", ts.URL, artigoID), nil, anaToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Ana artigo deveria sobreviver ao lote de Bob, status %d", resp.StatusCode)
	}
}

func TestIDOR_StoreScopedMethods(t *testing.T) {
	if !testDBAvailable {
		t.Skip("PostgreSQL indisponível")
	}
	// testa scoped store diretamente
	a, err := New(t.TempDir(), testPopplerDir, testDSN, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	// cria usuarios
	_, anaToken := ensureTestUser(t, a, "Ana Bagatinii")
	_ = anaToken
	bobID, _ := ensureTestUser(t, a, "StoreBob")
	ana, _ := a.DB.GetUsuarioByNome("Ana Bagatinii")
	if ana == nil {
		t.Fatal("Ana não encontrada")
	}
	// limpa artigos
	_, _ = a.DB.DB().Exec(`DELETE FROM artigos`)
	// cria artigos por usuario via ForUser
	idAna, _, err := a.DB.CreateArtigoForUser("Ana Store", "pdfs/a.pdf", ana.ID)
	if err != nil {
		t.Fatalf("CreateArtigoForUser Ana: %v", err)
	}
	idBob, _, err := a.DB.CreateArtigoForUser("Bob Store", "pdfs/b.pdf", bobID)
	if err != nil {
		t.Fatalf("CreateArtigoForUser Bob: %v", err)
	}
	// verifica client_id estável
	var cli sql.NullString
	err = a.DB.DB().QueryRow(`SELECT client_id FROM artigos WHERE id=$1`, idAna).Scan(&cli)
	if err != nil || !cli.Valid || cli.String == "" {
		t.Fatalf("client_id Ana não estável: %v %q", err, cli.String)
	}
	if cli.String != fmt.Sprintf("legacy-artigo-%d", idAna) {
		t.Errorf("client_id esperado legacy-artigo-%d veio %q", idAna, cli.String)
	}
	// ListArtigosByUser
	listAna, _ := a.DB.ListArtigosByUser("", ana.ID)
	if len(listAna) != 1 || listAna[0].ID != idAna {
		t.Fatalf("ListArtigosByUser Ana errado %v", listAna)
	}
	listBob, _ := a.DB.ListArtigosByUser("", bobID)
	if len(listBob) != 1 || listBob[0].ID != idBob {
		t.Fatalf("ListArtigosByUser Bob errado %v", listBob)
	}
	// GetArtigoByUser cross
	_, found, _ := a.DB.GetArtigoByUser(idAna, bobID)
	if found {
		t.Fatalf("Bob conseguiu GetArtigoByUser de Ana")
	}
	_, found, _ = a.DB.GetArtigoByUser(idBob, ana.ID)
	if found {
		t.Fatalf("Ana conseguiu GetArtigoByUser de Bob")
	}
	// busca filtrada por usuario
	listAnaBusca, _ := a.DB.ListArtigosByUser("Ana Store", ana.ID)
	if len(listAnaBusca) != 1 {
		t.Fatalf("busca filtrada Ana deveria 1 veio %d", len(listAnaBusca))
	}
	listBobBusca, _ := a.DB.ListArtigosByUser("Ana Store", bobID)
	if len(listBobBusca) != 0 {
		t.Fatalf("busca filtrada Bob com termo Ana deveria 0 veio %d", len(listBobBusca))
	}
	// ArtigoOwnedBy
	owned, _ := a.DB.ArtigoOwnedBy(idAna, ana.ID)
	if !owned {
		t.Fatalf("owned Ana Ana deveria true")
	}
	owned, _ = a.DB.ArtigoOwnedBy(idAna, bobID)
	if owned {
		t.Fatalf("owned Ana Bob deveria false")
	}
	can, _ := a.DB.CanAccessArticle(idBob, bobID)
	if !can {
		t.Fatalf("CanAccess Bob deveria true")
	}
	// marcacao ForUser grava usuario_id e client_id
	// precisa artigo paginas? cria marcacao direto
	m, err := a.DB.CreateMarcacaoForUser(idAna, ana.ID, 1, "highlight", "#FFEB3B", []byte("[]"), "texto")
	if err != nil {
		t.Fatalf("CreateMarcacaoForUser: %v", err)
	}
	var mu sql.NullInt64
	var mc sql.NullString
	err = a.DB.DB().QueryRow(`SELECT usuario_id, client_id FROM marcacoes WHERE id=$1`, m.ID).Scan(&mu, &mc)
	if err != nil || !mu.Valid || mu.Int64 != ana.ID {
		t.Fatalf("marcacao usuario_id errado %v %v", err, mu)
	}
	if !mc.Valid || mc.String != fmt.Sprintf("legacy-marcacao-%d", m.ID) {
		t.Errorf("marcacao client_id esperado legacy-marcacao-%d veio %q", m.ID, mc.String)
	}
	// nota ForUser
	n, err := a.DB.CreateNotaComTagsForUser(idBob, bobID, 1, "nota bob store", nil, []string{"tag1"}, "#FFEB3B")
	if err != nil {
		t.Fatalf("CreateNotaComTagsForUser: %v", err)
	}
	var nu sql.NullInt64
	var nc sql.NullString
	err = a.DB.DB().QueryRow(`SELECT usuario_id, client_id FROM notas WHERE id=$1`, n.ID).Scan(&nu, &nc)
	if err != nil || !nu.Valid || nu.Int64 != bobID {
		t.Fatalf("nota usuario_id errado %v %v", err, nu)
	}
	if !nc.Valid || !strings.HasPrefix(nc.String, "legacy-nota-") {
		t.Errorf("nota client_id inesperado %q", nc.String)
	}
	// habilita httptest para validar handlers isolados com store scope
	ts := httptest.NewServer(a.Routes())
	defer ts.Close()
	// gera tokens
	anaTok, _ := issueJWT(a.JWTSecret, ana.ID, "Ana Bagatinii")
	bobTok, _ := issueJWT(a.JWTSecret, bobID, "StoreBob")
	resp := doAsUser(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d", ts.URL, idAna), nil, bobTok)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("via HTTP Bob GET Ana deveria 404 veio %d", resp.StatusCode)
	}
	resp = doAsUser(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d", ts.URL, idAna), nil, anaTok)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("via HTTP Ana GET Ana deveria 200 veio %d", resp.StatusCode)
	}
	_ = idBob
}
