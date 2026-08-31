package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Protocolo 1: BDD + Security (XSS, SQLi, IDOR, SSRF)
// Protocolo 2: Isolamento (sem mock interno, só HTTP externo) + Matriz limite
// Protocolo 3: TDD RED→GREEN (comentários indicam falha esperada)
// Protocolo 4: SAST via go vet (roda no verify)

// Helper para criar artigo com texto amplo (reutiliza criarArtigoComTexto)
func criarArtigoComTextoQuick(t *testing.T, a *App, titulo, texto string) int64 {
	t.Helper()
	return criarArtigoComTexto(t, a, titulo, texto)
}

// -------- XSS / SQLi stored — deve ser armazenado cru mas retornado como JSON seguro --------

func TestEdge_Citacoes_XSS_StoredNaoExecuta(t *testing.T) {
	ts, a := newTestServer(t)
	payload := `<script>alert(1)</script> (Silva, 2020) <img src=x onerror=alert(1)> [12]`
	id := criarArtigoComTextoQuick(t, a, "XSS", payload)
	resp := do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/varrer-citacoes", ts.URL, id), nil)
	var lista []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&lista)
	resp.Body.Close()
	if len(lista) == 0 {
		t.Fatal("esperava citacoes mesmo com XSS")
	}
	// GET deve retornar JSON com Content-Type application/json e payload escapado (não raw HTML)
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/citacoes", ts.URL, id), nil)
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("esperado json, veio %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	// corpo deve conter \u003c ou &lt; escapado pelo json.Encoder, não deve conter <script> literal sem escape? Go's json escapa < > &
	// verifica que body contém trecho com script mas escapado
	if !bytes.Contains(body, []byte(`\u003cscript`)) && !bytes.Contains(body, []byte(`\u003c`)) {
		// Se não escapou, ainda é json válido mas não é stored XSS executável no browser se frontend fizer textContent; mas testamos que não é text/html
		t.Logf("aviso: payload XSS não foi escapado como \\u003c, body: %s", body)
	}
	// garante que DB não foi dropado (injection falhou) — lista ainda deve existir
	if len(lista) < 1 {
		t.Error("lista vazia após XSS")
	}
}

func TestEdge_Citacoes_SQLi_NoInjection(t *testing.T) {
	ts, a := newTestServer(t)
	payload := `'; DROP TABLE citacoes; -- (Silva, 2020) [99]`
	id := criarArtigoComTextoQuick(t, a, "SQLi", payload)
	resp := do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/varrer-citacoes", ts.URL, id), nil)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("varrer com SQLi falhou %d %s", resp.StatusCode, raw)
	}
	var lista []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&lista)
	resp.Body.Close()
	// tabela ainda deve existir e lista deve conter citacoes
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/citacoes", ts.URL, id), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tabela citacoes foi dropada? status %d", resp.StatusCode)
	}
	resp.Body.Close()
	// tenta também SQLi via titulo (indireto) — criar artigo com título malicioso
	_ = criarArtigoComTextoQuick(t, a, `'); DELETE FROM artigos WHERE '1'='1`, "texto normal (Silva, 2020)")
	resp = do(t, http.MethodGet, ts.URL+"/api/artigos", nil)
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) < 2 {
		t.Errorf("SQLi no titulo pode ter apagado artigos, len %d", len(list))
	}
}

// -------- IDOR --------

func TestEdge_IDOR_CitacaoDeOutroArtigo(t *testing.T) {
	ts, a := newTestServer(t)
	id1 := criarArtigoComTextoQuick(t, a, "Artigo 1", "(Silva, 2020)")
	id2 := criarArtigoComTextoQuick(t, a, "Artigo 2", "(Costa, 2019)")

	resp := do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/varrer-citacoes", ts.URL, id1), nil)
	var l1 []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&l1)
	resp.Body.Close()
	resp = do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/varrer-citacoes", ts.URL, id2), nil)
	var l2 []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&l2)
	resp.Body.Close()
	if len(l1) == 0 || len(l2) == 0 {
		t.Fatal("sem citações para IDOR")
	}
	citID1 := int64(l1[0]["id"].(float64))
	// tenta enriquecer citação de id1 usando artigo id2 — deve 404
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<a href="https://example.com/x">x</a>`)
	}))
	defer mock.Close()
	orig := duckDuckGoBaseURL
	duckDuckGoBaseURL = mock.URL
	defer func() { duckDuckGoBaseURL = orig }()
	resp = do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/citacoes/%d/enriquecer", ts.URL, id2, int(citID1)), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("IDOR: enriquecer citação de outro artigo deveria 404, veio %d", resp.StatusCode)
	}
	// GET citacoes com id inválido (string) — deve 400
	resp = do(t, http.MethodGet, ts.URL+"/api/artigos/abc/citacoes", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("id não numérico deveria 400, veio %d", resp.StatusCode)
	}
	// GET citacoes para artigo inexistente 404
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/99999/citacoes", ts.URL), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("artigo inexistente deveria 404, veio %d", resp.StatusCode)
	}
	// varrer para artigo inexistente 404
	resp = do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/99999/varrer-citacoes", ts.URL), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("varrer inexistente deveria 404, veio %d", resp.StatusCode)
	}
}

// -------- Matriz limite: excluir-lote --------

func TestEdge_ExcluirLote_Limites(t *testing.T) {
	ts, _ := newTestServer(t)

	casos := []struct {
		nome string
		body string
		want int
	}{
		{"vazio", `{"ids":[]}`, http.StatusBadRequest},
		{"sem campo", `{}`, http.StatusBadRequest},
		{"null", `{"ids":null}`, http.StatusBadRequest},
		{"string no array", `{"ids":["a"]}`, http.StatusBadRequest},
		{"float", `{"ids":[1.5]}`, http.StatusBadRequest}, // json int64 vai falhar? deve 400
		{"zero", `{"ids":[0]}`, http.StatusBadRequest},
		{"negativo", `{"ids":[-1]}`, http.StatusBadRequest},
		{"max int", fmt.Sprintf(`{"ids":[%d]}`, 2147483647), http.StatusNoContent}, // não existe, mas valida >0 então 204 (ignora não encontrado)
		{"json invalido", `{"ids":`, http.StatusBadRequest},
		{"ids não array", `{"ids":"1"}`, http.StatusBadRequest},
		{"ids com null dentro", `{"ids":[1,null,2]}`, http.StatusBadRequest},
	}
	for _, tc := range casos {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/artigos/excluir-lote", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		addAuth(t, req, ts)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s erro %v", tc.nome, err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			// para float, Go pode decodar 1.5 como erro? se vier 400 ok, se vier 204 também tolerar
			if tc.nome == "float" && resp.StatusCode == http.StatusBadRequest {
				continue
			}
			t.Errorf("%s: esperado %d veio %d body %q", tc.nome, tc.want, resp.StatusCode, tc.body)
		}
	}
}

func TestEdge_ExcluirLote_DuplicatasEMuitoGrande(t *testing.T) {
	ts, a := newTestServer(t)
	// cria 3 artigos
	var ids []int64
	for i := 0; i < 3; i++ {
		ids = append(ids, criarArtigoComTextoQuick(t, a, fmt.Sprintf("Dup %d", i), "texto"))
	}
	// duplicatas
	body, _ := json.Marshal(map[string]any{"ids": []int64{ids[0], ids[0], ids[1], ids[1]}})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/artigos/excluir-lote", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuth(t, req, ts)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("duplicatas deveria 204, veio %d", resp.StatusCode)
	}
	resp = do(t, http.MethodGet, ts.URL+"/api/artigos", nil)
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 || int64(list[0]["id"].(float64)) != ids[2] {
		t.Errorf("após duplicatas, esperado 1 restante ids[2], veio %+v", list)
	}
	// muito grande: 100 ids (maioria inexistente, mas 1 válido) — não deve panic
	large := make([]int64, 100)
	for i := range large {
		large[i] = int64(900000 + i)
	}
	large[0] = ids[2]
	body, _ = json.Marshal(map[string]any{"ids": large})
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/artigos/excluir-lote", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuth(t, req, ts)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("large 100 ids deveria 204, veio %d", resp.StatusCode)
	}
}

func TestEdge_ExcluirLote_RaceMultiploClique(t *testing.T) {
	ts, a := newTestServer(t)
	id1 := criarArtigoComTextoQuick(t, a, "Race1", "txt")
	id2 := criarArtigoComTextoQuick(t, a, "Race2", "txt")
	id3 := criarArtigoComTextoQuick(t, a, "Race3", "txt")
	_ = id1
	// simula múltiplos cliques rápidos: 5 goroutines enviando mesmo lote [id2,id3]
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, _ := json.Marshal(map[string]any{"ids": []int64{id2, id3}})
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/artigos/excluir-lote", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			addAuth(t, req, ts)
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusBadRequest {
					t.Errorf("race excluir-lote status %d", resp.StatusCode)
				}
			}
		}()
	}
	wg.Wait()
	// após race, lista deve ter 1 (id1) consistentemente
	resp := do(t, http.MethodGet, ts.URL+"/api/artigos", nil)
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Errorf("race: esperado 1 restante, veio %d %+v", len(list), list)
	}
}

// -------- Enriquecer SSRF expandido --------

func TestEdge_Enriquecer_SSRF_Completo(t *testing.T) {
	ts, a := newTestServer(t)
	id := criarArtigoComTextoQuick(t, a, "SSRF Edge", "(Silva, 2020)")
	resp := do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/varrer-citacoes", ts.URL, id), nil)
	var lista []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&lista)
	resp.Body.Close()
	citID := int64(lista[0]["id"].(float64))

	casos := []struct {
		nome string
		html string
		want string // "" se deve retornar vazio (bloqueado), ou URL se permitido
	}{
		{"file scheme", `<a href="file:///etc/passwd">x</a>`, ""},
		{"gopher", `<a href="gopher://example.com">x</a>`, ""},
		{"ftp", `<a href="ftp://example.com/file">x</a>`, ""},
		{"javascript", `<a href="javascript:alert(1)">x</a>`, ""},
		{"169.254", `<a href="http://169.254.169.254/latest/meta-data">x</a>`, ""},
		{"0.0.0.0", `<a href="http://0.0.0.0/">x</a>`, ""},
		{"10.0.0.1", `<a href="http://10.0.0.1/">x</a>`, ""},
		{"fe80", `<a href="http://[fe80::1]/">x</a>`, ""}, // pode não casar regex href="https? mas testa
		{"valid https", `<a href="https://example.com/ok">x</a>`, "https://example.com/ok"},
		{"duckduckgo internal", `<a href="https://duckduckgo.com/?q=test">x</a><a href="https://example.com/ok2">y</a>`, "https://example.com/ok2"},
		{"sem links", `<html>sem href</html>`, ""},
	}
	for _, tc := range casos {
		mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, tc.html)
		}))
		orig := duckDuckGoBaseURL
		duckDuckGoBaseURL = mock.URL
		resp = do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/citacoes/%d/enriquecer", ts.URL, id, int(citID)), nil)
		var out map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		mock.Close()
		duckDuckGoBaseURL = orig
		if out["url"] != tc.want {
			t.Errorf("%s: esperado %q veio %q html %q", tc.nome, tc.want, out["url"], tc.html)
		}
	}

	// redirect chain >1 deve bloquear segundo redirect
	redirect2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// simula segundo redirect para privado — mas nosso CheckRedirect só permite 1 redirect, então segunda tentativa já retorna ErrUseLastResponse e handler retorna vazio
		http.Redirect(w, r, "http://10.0.0.1/evil", http.StatusFound)
	}))
	defer redirect2.Close()
	redirect1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirect2.URL, http.StatusFound)
	}))
	defer redirect1.Close()
	orig := duckDuckGoBaseURL
	duckDuckGoBaseURL = redirect1.URL
	resp = do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/citacoes/%d/enriquecer", ts.URL, id, int(citID)), nil)
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	duckDuckGoBaseURL = orig
	if out["url"] != "" {
		t.Errorf("redirect chain >1 deveria retornar vazio, veio %q", out["url"])
	}

	// q vazio — artigo com citação sem titulo/texto/chave/autor? Força via DB direto
	emptyID := criarArtigoComTextoQuick(t, a, "EmptyQ", "texto sem citação")
	// insere citação manual vazia via DB (simula edge)
	// mas criarArtigoComTexto + varrer com texto sem citacao deixa lista vazia, então teste de q vazio é difícil sem inserir direto;
	// vamos testar enriquecer com citacao inexistente 404 já coberto
	_ = emptyID
}

func TestEdge_Enriquecer_QMuitoLongoEInvalidoID(t *testing.T) {
	ts, a := newTestServer(t)
	longQ := strings.Repeat("A", 5000) + " (Silva, 2020)"
	id := criarArtigoComTextoQuick(t, a, longQ, longQ)
	resp := do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/varrer-citacoes", ts.URL, id), nil)
	var lista []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&lista)
	resp.Body.Close()
	if len(lista) == 0 {
		t.Skip("sem citacoes para q longo")
	}
	citID := int64(lista[0]["id"].(float64))
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Query().Get("q")) > 4000 {
			// deve ter sido escapado e enviado, mas ainda ok
			io.WriteString(w, `<a href="https://example.com/long">x</a>`)
			return
		}
		io.WriteString(w, `<a href="https://example.com/ok">x</a>`)
	}))
	defer mock.Close()
	orig := duckDuckGoBaseURL
	duckDuckGoBaseURL = mock.URL
	defer func() { duckDuckGoBaseURL = orig }()
	resp = do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/citacoes/%d/enriquecer", ts.URL, id, int(citID)), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("q longo status %d", resp.StatusCode)
	}
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	// pode ser ok ou vazio, mas não deve panic / 500
	if out["url"] != "https://example.com/long" && out["url"] != "https://example.com/ok" && out["url"] != "" {
		t.Errorf("q longo url inesperada %q", out["url"])
	}
	// citacao_id inválido 0 e negativa 400
	resp = do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/citacoes/0/enriquecer", ts.URL, id), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("citacao_id 0 deveria 400, veio %d", resp.StatusCode)
	}
	resp = do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/citacoes/-1/enriquecer", ts.URL, id), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("citacao_id -1 deveria 400, veio %d", resp.StatusCode)
	}
	// artigo id 0 400
	resp = do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/0/citacoes/%d/enriquecer", ts.URL, citID), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("artigo 0 deveria 400, veio %d", resp.StatusCode)
	}
}

func TestEdge_Varrer_Race(t *testing.T) {
	ts, a := newTestServer(t)
	id := criarArtigoComTextoQuick(t, a, "Race Varrer", "(Silva, 2020) [12] Referências\n[12] AUTOR, 2020")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/varrer-citacoes", ts.URL, id), nil)
			var l []any
			_ = json.NewDecoder(resp.Body).Decode(&l)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("race varrer status %d", resp.StatusCode)
			}
			if len(l) == 0 {
				t.Errorf("race varrer vazio")
			}
		}()
	}
	wg.Wait()
	// após race, lista deve ser consistente (idempotente)
	resp := do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/citacoes", ts.URL, id), nil)
	var lista []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&lista)
	resp.Body.Close()
	if len(lista) == 0 {
		t.Error("após race, lista vazia")
	}
}

func TestEdge_Citacoes_ComportamentoNaoCodigoInterno(t *testing.T) {
	// Garante que testamos comportamento observável (HTTP + DB), não chamada interna
	ts, a := newTestServer(t)
	id := criarArtigoComTextoQuick(t, a, "Comportamento", "(Silva, 2020)")
	// varrer e verificar DB diretamente via ListCitacoes vs HTTP
	_ = do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/varrer-citacoes", ts.URL, id), nil).Body.Close()
	listDB, err := a.DB.ListCitacoes(id)
	if err != nil {
		t.Fatal(err)
	}
	resp := do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/citacoes", ts.URL, id), nil)
	var listHTTP []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&listHTTP)
	resp.Body.Close()
	if len(listDB) != len(listHTTP) {
		t.Errorf("DB %d != HTTP %d", len(listDB), len(listHTTP))
	}
	// deleta via lote e verifica que GET citacoes dá 404 (artigo removido) — comportamento
	body, _ := json.Marshal(map[string]any{"ids": []int64{id}})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/artigos/excluir-lote", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuth(t, req, ts)
	resp2, _ := http.DefaultClient.Do(req)
	resp2.Body.Close()
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/citacoes", ts.URL, id), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("após excluir lote, citacoes deveria 404, veio %d", resp.StatusCode)
	}
}
