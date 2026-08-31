package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"artigos-ana/backend/internal/store"
)

func TestBuscaBiblioteca(t *testing.T) {
	ts, a := newTestServer(t)
	criado := uploadPDF(t, ts, "Silva 2020 Artigo Especial")
	id := int64(criado["id"].(float64))

	// cria nota com texto contendo termo
	resp := postJSON(t, fmt.Sprintf("%s/api/artigos/%d/notas", ts.URL, id), map[string]any{
		"pagina": 1, "texto": "nota sobre Silva",
	})
	resp.Body.Close()
	// insere citação diretamente via store para testar busca por citacao
	ano := 2020
	if _, err := a.DB.ReplaceCitacoes(id, []store.Citacao{{Tipo: "autor_ano", Chave: "(Sinergia, 2020)", Autor: "Sinergia", Ano: &ano, Trecho: "x", Titulo: "t", Texto: "x"}}); err != nil {
		t.Fatalf("inserir citacao: %v", err)
	}

	// busca por titulo
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos?busca=Silva", ts.URL), nil)
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("busca Silva deveria retornar 1, veio %d %v", len(list), list)
	}
	// case-insensitive
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos?busca=silva", ts.URL), nil)
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("busca case-insensitive falhou %v", list)
	}
	// busca por conteúdo de nota
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos?busca=nota%%20sobre", ts.URL), nil)
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("busca nota falhou %v", list)
	}
	// busca por autor de citação
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos?busca=Sinergia", ts.URL), nil)
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("busca citacao autor falhou %v", list)
	}
	// busca inexistente retorna vazio 200
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos?busca=ZZZnaoexiste", ts.URL), nil)
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 0 {
		t.Fatalf("busca inexistente deveria retornar 0, veio %d", len(list))
	}
}

func TestBuscaPDF(t *testing.T) {
	ts, _ := newTestServer(t)
	criado := uploadPDF(t, ts, "Artigo Busca")
	id := int64(criado["id"].(float64))
	resp := do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/busca?q=mundo", ts.URL, id), nil)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("busca mundo status %d %s", resp.StatusCode, raw)
	}
	var res []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&res)
	resp.Body.Close()
	if len(res) == 0 {
		t.Fatalf("busca mundo deveria retornar >=1, veio 0")
	}
	if res[0]["pagina"] != float64(1) {
		t.Fatalf("pagina esperada 1, veio %v", res[0])
	}
	if pos, ok := res[0]["pos"]; !ok || pos == nil {
		t.Fatalf("pos ausente %v", res[0])
	} else {
		arr, _ := pos.([]any)
		if len(arr) != 4 {
			t.Fatalf("pos deveria ter 4 coords, veio %v", pos)
		}
	}
	if trecho, ok := res[0]["trecho"].(string); !ok || len(trecho) == 0 || len([]rune(trecho)) > 150 {
		t.Fatalf("trecho inválido %v", res[0])
	}
	// case-insensitive
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/busca?q=MUNDO", ts.URL, id), nil)
	_ = json.NewDecoder(resp.Body).Decode(&res)
	resp.Body.Close()
	if len(res) == 0 {
		t.Fatalf("busca case-insensitive falhou")
	}
	// q muito curto <2 => 400
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/busca?q=a", ts.URL, id), nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("q curto deveria ser 400, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	// q vazio => 400
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/busca?q=", ts.URL, id), nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("q vazio deveria ser 400, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	// artigo inexistente => 404
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/9999/busca?q=mundo", ts.URL), nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("artigo inexistente deveria ser 404, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestNotasTagsCor(t *testing.T) {
	ts, _ := newTestServer(t)
	criado := uploadPDF(t, ts, "Artigo Notas Tags")
	id := int64(criado["id"].(float64))

	resp := postJSON(t, fmt.Sprintf("%s/api/artigos/%d/notas", ts.URL, id), map[string]any{
		"pagina": 1, "texto": "nota com tag", "tags": []string{"revisar", "importante"}, "cor": "#FF0000",
	})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("criar nota com tags status %d %s", resp.StatusCode, raw)
	}
	var nota map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&nota)
	resp.Body.Close()
	notaID := int64(nota["id"].(float64))
	if nota["cor"] != "#FF0000" {
		t.Fatalf("cor esperada #FF0000, veio %v", nota["cor"])
	}
	if tags, ok := nota["tags"].([]any); !ok || len(tags) != 2 {
		t.Fatalf("tags esperadas 2, veio %v", nota["tags"])
	}
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/notas?tag=revisar", ts.URL, id), nil)
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("filtro por tag revisiar deveria retornar 1, veio %d %v", len(list), list)
	}
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/notas?cor=%%23FF0000", ts.URL, id), nil)
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("filtro por cor deveria retornar 1, veio %d", len(list))
	}
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/notas?tag=inexistente", ts.URL, id), nil)
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 0 {
		t.Fatalf("filtro tag inexistente deveria 0, veio %d", len(list))
	}
	body, _ := json.Marshal(map[string]any{"tags": []string{"revisar", "duvida"}, "cor": "#00FF00"})
	req, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/artigos/%d/notas/%d", ts.URL, id, notaID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuth(t, req, ts)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("PATCH status %d %s", resp.StatusCode, raw)
	}
	_ = json.NewDecoder(resp.Body).Decode(&nota)
	resp.Body.Close()
	if nota["cor"] != "#00FF00" {
		t.Fatalf("cor após patch esperada #00FF00, veio %v", nota["cor"])
	}
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/notas", ts.URL, id), nil)
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 || list[0]["cor"] != "#00FF00" {
		t.Fatalf("persistência após patch falhou %v", list)
	}
	body, _ = json.Marshal(map[string]any{"tags": []string{"bad,tag"}})
	req, _ = http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/artigos/%d/notas/%d", ts.URL, id, notaID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuth(t, req, ts)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("tag com vírgula deveria ser 400, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	body, _ = json.Marshal(map[string]any{"cor": "red"})
	req, _ = http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/artigos/%d/notas/%d", ts.URL, id, notaID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuth(t, req, ts)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("cor inválida deveria ser 400, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	body, _ = json.Marshal(map[string]any{"cor": "#123456"})
	req, _ = http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/artigos/%d/notas/9999", ts.URL, id), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuth(t, req, ts)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("nota inexistente deveria ser 404, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSumario(t *testing.T) {
	ts, a := newTestServer(t)
	artigoID, _, err := a.DB.CreateArtigo("Artigo Sumario", "pdfs/1.pdf")
	if err != nil {
		t.Fatal(err)
	}
	words1 := []map[string]any{
		{"texto": "1.", "x0": 250.0, "y0": 100.0, "x1": 270.0, "y1": 115.0},
		{"texto": "Introdução", "x0": 280.0, "y0": 100.0, "x1": 380.0, "y1": 115.0},
		{"texto": "Este", "x0": 70.0, "y0": 150.0, "x1": 100.0, "y1": 165.0},
		{"texto": "é", "x0": 110.0, "y0": 150.0, "x1": 120.0, "y1": 165.0},
		{"texto": "conteúdo", "x0": 130.0, "y0": 150.0, "x1": 200.0, "y1": 165.0},
	}
	b1, _ := json.Marshal(words1)
	if err := a.DB.AddPagina(artigoID, 1, "paginas/1/1.png", b1, 600, 800); err != nil {
		t.Fatal(err)
	}
	words2 := []map[string]any{
		{"texto": "2.", "x0": 250.0, "y0": 100.0, "x1": 270.0, "y1": 115.0},
		{"texto": "Metodologia", "x0": 280.0, "y0": 100.0, "x1": 400.0, "y1": 115.0},
	}
	b2, _ := json.Marshal(words2)
	if err := a.DB.AddPagina(artigoID, 2, "paginas/1/2.png", b2, 600, 800); err != nil {
		t.Fatal(err)
	}
	words3 := []map[string]any{
		{"texto": "Conclusão", "x0": 250.0, "y0": 100.0, "x1": 350.0, "y1": 115.0},
	}
	b3, _ := json.Marshal(words3)
	if err := a.DB.AddPagina(artigoID, 3, "paginas/1/3.png", b3, 600, 800); err != nil {
		t.Fatal(err)
	}
	resp := do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/sumario", ts.URL, artigoID), nil)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("sumario status %d %s", resp.StatusCode, raw)
	}
	var items []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&items)
	resp.Body.Close()
	if len(items) != 3 {
		t.Fatalf("sumario deveria ter 3 itens, veio %d %v", len(items), items)
	}
	if items[0]["titulo"] != "1. Introdução" || items[0]["pagina"] != float64(1) || items[0]["ordem"] != float64(1) {
		t.Fatalf("item 0 errado %v", items[0])
	}
	if items[1]["pagina"] != float64(2) || items[2]["pagina"] != float64(3) {
		t.Fatalf("ordem páginas errada %v", items)
	}
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/9999/sumario", ts.URL), nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("sumario inexistente deveria ser 404, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	criado := uploadPDF(t, ts, "Artigo sem sumario")
	id := int64(criado["id"].(float64))
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/sumario", ts.URL, id), nil)
	_ = json.NewDecoder(resp.Body).Decode(&items)
	resp.Body.Close()
	if items == nil {
		t.Fatalf("sumario deveria retornar array, veio null")
	}
}

func TestEdgeXSS_IDOR_Limite(t *testing.T) {
	ts, a := newTestServer(t)
	// cria dois artigos
	c1 := uploadPDF(t, ts, "Artigo A")
	id1 := int64(c1["id"].(float64))
	c2 := uploadPDF(t, ts, "Artigo B")
	id2 := int64(c2["id"].(float64))
	// nota em A
	resp := postJSON(t, fmt.Sprintf("%s/api/artigos/%d/notas", ts.URL, id1), map[string]any{"pagina": 1, "texto": "segredo"})
	var nota map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&nota)
	resp.Body.Close()
	notaID := int64(nota["id"].(float64))
	// IDOR: tenta acessar nota de A via B
	body, _ := json.Marshal(map[string]any{"cor": "#123456"})
	req, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/artigos/%d/notas/%d", ts.URL, id2, notaID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuth(t, req, ts)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("IDOR: deveria ser 404, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	// IDOR via DELETE
	resp = do(t, http.MethodDelete, fmt.Sprintf("%s/api/artigos/%d/notas/%d", ts.URL, id2, notaID), nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("IDOR delete deveria ser 404, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	// XSS busca
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos?busca=%%3Cscript%%3Ealert(1)%%3C/script%%3E", ts.URL), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("XSS busca deveria ser 200, veio %d", resp.StatusCode)
	}
	var list []any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	// deve retornar 200 com lista (vazia) sem crash
	hugeRunes := make([]rune, 11000)
	for i := range hugeRunes {
		hugeRunes[i] = 'a'
	}
	hugeStr := string(hugeRunes)
	resp = postJSON(t, fmt.Sprintf("%s/api/artigos/%d/notas", ts.URL, id1), map[string]any{"pagina": 1, "texto": hugeStr})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("texto 11k deveria ser 400, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	// PATCH com texto 11k
	body, _ = json.Marshal(map[string]any{"texto": hugeStr})
	req, _ = http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/artigos/%d/notas/%d", ts.URL, id1, notaID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuth(t, req, ts)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PATCH texto 11k deveria ser 400, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	longQRunes := make([]rune, 101)
	for i := range longQRunes {
		longQRunes[i] = 'b'
	}
	longQ := string(longQRunes)
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/busca?q=%s", ts.URL, id1, longQ), nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("q 101 chars deveria ser 400, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	// verifica que cor e tag filtro com XSS não quebra
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/notas?tag=%%3Cscript%%3E", ts.URL, id1), nil)
	// tag com <script> contém < > mas não ,; então deve ser 200? Nossa validação rejeita ,; apenas, então <script> passa como tag literal mas não encontra nada => 200 vazio
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("tag XSS status inesperado %d", resp.StatusCode)
	}
	resp.Body.Close()
	_ = a
}
