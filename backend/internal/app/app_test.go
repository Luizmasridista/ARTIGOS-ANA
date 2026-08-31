package app

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"artigos-ana/backend/internal/store"
)

var (
	testPopplerDir string
	testPDFPath    string
	testDSN        string
)

var testDBAvailable bool

func TestMain(m *testing.M) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	wd, _ := os.Getwd()
	testPopplerDir = filepath.Join(wd, "..", "..", "bin", "poppler")
	testPDFPath = filepath.Join(wd, "..", "..", "testdata", "artigo-teste.pdf")
	cfg := store.Config{
		Host:     "localhost",
		Port:     "5432",
		User:     "postgres",
		Password: "Dudu1408@@",
		Database: "artigos_ana_test",
	}
	if err := setupTestDB(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: banco de teste indisponível (%v) — testes unitários sem DB continuarão, integração será skip\n", err)
		testDBAvailable = false
		testDSN = ""
	} else {
		testDBAvailable = true
		testDSN = store.BuildDSN(cfg, "")
	}
	os.Exit(m.Run())
}

func requireDB(t *testing.T) {
	t.Helper()
	if !testDBAvailable || testDSN == "" {
		t.Skip("PostgreSQL indisponível — teste de integração separado")
	}
}

func setupTestDB(cfg store.Config) error {
	m := cfg
	m.Database = "postgres"
	db, err := sql.Open("pgx", store.BuildDSN(m, ""))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return err
	}
	if _, err := db.Exec(`DROP DATABASE IF EXISTS ` + cfg.Database); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE DATABASE ` + cfg.Database); err != nil {
		return err
	}
	st, err := store.Open(store.BuildDSN(cfg, ""))
	if err != nil {
		return err
	}
	return st.Close()
}

func wipeDB(t *testing.T) {
	t.Helper()
	requireDB(t)
	db, err := sql.Open("pgx", testDSN)
	if err != nil {
		t.Fatalf("abrir banco de teste: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM historico`); err != nil {
		t.Fatalf("limpar histórico: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM artigos`); err != nil {
		t.Fatalf("limpar artigos: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM login_tentativas`); err != nil {
		t.Fatalf("limpar login_tentativas: %v", err)
	}
}

var testAuthCache = map[string]string{}

func testLogin(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	if v, ok := testAuthCache[ts.URL]; ok {
		return v
	}
	body, _ := json.Marshal(map[string]string{"nome": "Ana Bagatinii"})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("login falhou: status %d corpo %s", resp.StatusCode, raw)
	}
	cookie := ""
	for _, c := range resp.Cookies() {
		if c.Name == "ana_session" {
			cookie = c.Value
			break
		}
	}
	if cookie == "" {
		// fallback parse Set-Cookie header
		h := resp.Header.Get("Set-Cookie")
		if idx := strings.Index(h, "ana_session="); idx >= 0 {
			rest := h[idx+len("ana_session="):]
			if semi := strings.Index(rest, ";"); semi >= 0 {
				cookie = rest[:semi]
			} else {
				cookie = rest
			}
		}
	}
	if cookie == "" {
		t.Fatalf("cookie ana_session não retornado no login")
	}
	testAuthCache[ts.URL] = cookie
	return cookie
}

func addAuth(t *testing.T, req *http.Request, ts *httptest.Server) {
	t.Helper()
	cookie := testLogin(t, ts)
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
}

var currentTS *httptest.Server

func newTestServer(t *testing.T) (*httptest.Server, *App) {
	t.Helper()
	wipeDB(t)
	a, err := New(t.TempDir(), testPopplerDir, testDSN, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	ts := httptest.NewServer(a.Routes())
	t.Cleanup(ts.Close)
	currentTS = ts
	// pre-warm auth cache for this server
	testLogin(t, ts)
	return ts, a
}

func do(t *testing.T, method, url string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	// adiciona cookie de sessão para rotas protegidas, se existir servidor de teste
	if currentTS != nil && strings.Contains(url, "/api/") && !strings.Contains(url, "/api/auth/") && !strings.HasSuffix(url, "/api/health") && !strings.HasSuffix(url, "/health") && !strings.HasSuffix(url, "/api/info") {
		// tenta anexar cookie do servidor correspondente
		for k, v := range testAuthCache {
			if strings.HasPrefix(url, k) {
				req.AddCookie(&http.Cookie{Name: "ana_session", Value: v})
				break
			}
		}
		// fallback para currentTS
		if len(req.Cookies()) == 0 {
			if c, ok := testAuthCache[currentTS.URL]; ok {
				req.AddCookie(&http.Cookie{Name: "ana_session", Value: c})
			}
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func uploadPDF(t *testing.T, ts *httptest.Server, titulo string) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "artigo-teste.pdf")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(testPDFPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	if titulo != "" {
		if err := mw.WriteField("titulo", titulo); err != nil {
			t.Fatal(err)
		}
	}
	mw.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/artigos", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	addAuth(t, req, ts)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload: status %d, corpo: %s", resp.StatusCode, raw)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func postJSON(t *testing.T, url string, v any) *http.Response {
	t.Helper()
	body, _ := json.Marshal(v)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	// anexa auth se URL for de API protegida
	if currentTS != nil && strings.Contains(url, "/api/") && !strings.Contains(url, "/api/auth/") && !strings.HasSuffix(url, "/api/health") {
		for k, v := range testAuthCache {
			if strings.HasPrefix(url, k) {
				req.AddCookie(&http.Cookie{Name: "ana_session", Value: v})
				break
			}
		}
		if len(req.Cookies()) == 0 {
			if c, ok := testAuthCache[currentTS.URL]; ok {
				req.AddCookie(&http.Cookie{Name: "ana_session", Value: c})
			}
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestAtualizarCorMarcacao(t *testing.T) {
	ts, _ := newTestServer(t)
	criado := uploadPDF(t, ts, "Artigo Cor")
	id := int64(criado["id"].(float64))

	resp := postJSON(t, fmt.Sprintf("%s/api/artigos/%d/marcacoes", ts.URL, id), map[string]any{
		"pagina":   1,
		"tipo":     "highlight",
		"cor":      "#FFEB3B",
		"palavras": [][]float64{{70, 87, 137, 102}},
		"texto":    "trecho",
	})
	var marc map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&marc)
	resp.Body.Close()
	marcID := int64(marc["id"].(float64))

	body, _ := json.Marshal(map[string]string{"cor": "#9EE6A8"})
	req, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/artigos/%d/marcacoes/%d", ts.URL, id, marcID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuth(t, req, ts)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PATCH: status %d", resp.StatusCode)
	}

	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/marcacoes", ts.URL, id), nil)
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 || list[0]["cor"] != "#9EE6A8" {
		t.Fatalf("cor não atualizada: %v", list)
	}

	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/historico", ts.URL, id), nil)
	var hist []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&hist)
	resp.Body.Close()
	achou := false
	for _, ev := range hist {
		if ev["entidade"] == "marcacao" && ev["acao"] == "atualizar" {
			achou = true
		}
	}
	if !achou {
		t.Fatalf("histórico sem evento marcacao/atualizar: %v", hist)
	}

	body, _ = json.Marshal(map[string]string{"cor": "#8FC1FF"})
	req, _ = http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/artigos/%d/marcacoes/999", ts.URL, id), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuth(t, req, ts)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("PATCH inexistente: status %d, esperado 404", resp.StatusCode)
	}
}

func TestHealth(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := do(t, http.MethodGet, ts.URL+"/health", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: status %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true || out["versao"] != "0.1.0" {
		t.Fatalf("health inesperado: %v", out)
	}
}

func TestUploadListGetDelete(t *testing.T) {
	ts, a := newTestServer(t)
	criado := uploadPDF(t, ts, "Artigo de Teste")
	if criado["titulo"] != "Artigo de Teste" {
		t.Fatalf("titulo errado: %v", criado["titulo"])
	}
	if criado["num_paginas"] != float64(2) {
		t.Fatalf("num_paginas errado: %v", criado["num_paginas"])
	}
	id := int64(criado["id"].(float64))

	resp := do(t, http.MethodGet, ts.URL+"/api/artigos", nil)
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("esperado 1 artigo na lista, veio %d", len(list))
	}
	if list[0]["titulo"] != "Artigo de Teste" || list[0]["num_paginas"] != float64(2) {
		t.Fatalf("lista errada: %v", list[0])
	}

	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d", ts.URL, id), nil)
	var detalhe struct {
		Titulo  string `json:"titulo"`
		Paginas []struct {
			Numero  int `json:"numero"`
			Largura int `json:"largura"`
			Altura  int `json:"altura"`
		} `json:"paginas"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&detalhe)
	resp.Body.Close()
	if detalhe.Titulo != "Artigo de Teste" || len(detalhe.Paginas) != 2 {
		t.Fatalf("detalhe errado: %+v", detalhe)
	}
	if detalhe.Paginas[0].Numero != 1 || detalhe.Paginas[0].Largura <= 0 || detalhe.Paginas[0].Altura <= 0 {
		t.Fatalf("página 1 errada: %+v", detalhe.Paginas[0])
	}
	if _, err := os.Stat(filepath.Join(a.pdfsDir, fmt.Sprintf("%d.pdf", id))); err != nil {
		t.Fatalf("pdf não salvo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.paginasPath(id), "1.png")); err != nil {
		t.Fatalf("png da página 1 não salvo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.paginasPath(id), "1.camada.json")); err != nil {
		t.Fatalf("camada.json da página 1 não salvo: %v", err)
	}

	resp = do(t, http.MethodGet, ts.URL+"/api/artigos/999", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("esperado 404, veio %d", resp.StatusCode)
	}

	resp = do(t, http.MethodDelete, fmt.Sprintf("%s/api/artigos/%d", ts.URL, id), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(a.pdfsDir, fmt.Sprintf("%d.pdf", id))); !os.IsNotExist(err) {
		t.Fatalf("pdf não foi removido do disco")
	}
	if _, err := os.Stat(a.paginasPath(id)); !os.IsNotExist(err) {
		t.Fatalf("pasta de páginas não foi removida")
	}
	resp = do(t, http.MethodGet, ts.URL+"/api/artigos", nil)
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 0 {
		t.Fatalf("lista deveria estar vazia após delete")
	}
}

func TestPaginaImagemECamada(t *testing.T) {
	ts, _ := newTestServer(t)
	criado := uploadPDF(t, ts, "Artigo Camada")
	id := int64(criado["id"].(float64))

	resp := do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/paginas/1/imagem", ts.URL, id), nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("imagem: status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type errado: %s", ct)
	}
	img, _ := io.ReadAll(resp.Body)
	if len(img) < 24 || string(img[1:4]) != "PNG" {
		t.Fatalf("resposta não é um PNG")
	}

	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/paginas/1/camada", ts.URL, id), nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("camada: status %d", resp.StatusCode)
	}
	var camada struct {
		Largura  int `json:"largura"`
		Altura   int `json:"altura"`
		Palavras []struct {
			Texto string  `json:"texto"`
			X0    float64 `json:"x0"`
			Y0    float64 `json:"y0"`
			X1    float64 `json:"x1"`
			Y1    float64 `json:"y1"`
		} `json:"palavras"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&camada); err != nil {
		t.Fatal(err)
	}
	if camada.Largura <= 0 || camada.Altura <= 0 {
		t.Fatalf("dimensões da camada inválidas: %dx%d", camada.Largura, camada.Altura)
	}
	if len(camada.Palavras) == 0 {
		t.Fatalf("camada sem palavras")
	}
	found := false
	for _, p := range camada.Palavras {
		if p.Texto == "" || p.X1 <= p.X0 || p.Y1 <= p.Y0 {
			t.Fatalf("palavra com dados inválidos: %+v", p)
		}
		if strings.Contains(p.Texto, "mundo") {
			found = true
		}
	}
	if !found {
		t.Fatalf("palavra 'mundo' não encontrada na camada: %+v", camada.Palavras)
	}
}

func TestMarcacoesENotas(t *testing.T) {
	ts, _ := newTestServer(t)
	criado := uploadPDF(t, ts, "Artigo Marcado")
	id := int64(criado["id"].(float64))

	resp := postJSON(t, fmt.Sprintf("%s/api/artigos/%d/marcacoes", ts.URL, id), map[string]any{
		"pagina":   1,
		"tipo":     "highlight",
		"cor":      "#FFEB3B",
		"palavras": [][]float64{{10.5, 20.1, 40.2, 32.8}},
		"texto":    "trecho marcado",
	})
	var marcacao map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&marcacao)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("criar marcação: status %d", resp.StatusCode)
	}
	if marcacao["cor"] != "#FFEB3B" || marcacao["texto"] != "trecho marcado" {
		t.Fatalf("marcação errada: %v", marcacao)
	}
	marcID := int64(marcacao["id"].(float64))

	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/marcacoes", ts.URL, id), nil)
	var marcacoes []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&marcacoes)
	resp.Body.Close()
	if len(marcacoes) != 1 || marcacoes[0]["palavras"].([]any)[0].([]any)[0] != 10.5 {
		t.Fatalf("lista de marcações errada: %v", marcacoes)
	}

	resp = postJSON(t, fmt.Sprintf("%s/api/artigos/%d/notas", ts.URL, id), map[string]any{
		"pagina": 1,
		"texto":  "minha nota",
	})
	var nota map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&nota)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || nota["texto"] != "minha nota" {
		t.Fatalf("criar nota falhou: %d %v", resp.StatusCode, nota)
	}
	notaID := int64(nota["id"].(float64))

	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/notas", ts.URL, id), nil)
	var notas []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&notas)
	resp.Body.Close()
	if len(notas) != 1 || notas[0]["pagina"] != float64(1) {
		t.Fatalf("lista de notas errada: %v", notas)
	}

	resp = do(t, http.MethodDelete, fmt.Sprintf("%s/api/artigos/%d/marcacoes/%d", ts.URL, id, marcID), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete marcação: %d", resp.StatusCode)
	}
	resp = do(t, http.MethodDelete, fmt.Sprintf("%s/api/artigos/%d/notas/%d", ts.URL, id, notaID), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete nota: %d", resp.StatusCode)
	}
}

func TestHistorico(t *testing.T) {
	ts, _ := newTestServer(t)
	criado := uploadPDF(t, ts, "Artigo Historico")
	id := int64(criado["id"].(float64))

	resp := postJSON(t, fmt.Sprintf("%s/api/artigos/%d/marcacoes", ts.URL, id), map[string]any{
		"pagina":   1,
		"tipo":     "highlight",
		"cor":      "#FFEB3B",
		"palavras": [][]float64{{10.5, 20.1, 40.2, 32.8}},
		"texto":    "trecho marcado",
	})
	var marcacao map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&marcacao)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("criar marcação: status %d", resp.StatusCode)
	}
	marcID := int64(marcacao["id"].(float64))

	resp = do(t, http.MethodDelete, fmt.Sprintf("%s/api/artigos/%d/marcacoes/%d", ts.URL, id, marcID), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete marcação: %d", resp.StatusCode)
	}

	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/historico", ts.URL, id), nil)
	var hist []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&hist)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("historico: status %d", resp.StatusCode)
	}
	if len(hist) != 4 {
		t.Fatalf("esperado 4 eventos no histórico, veio %d: %v", len(hist), hist)
	}
	byID := map[string]map[string]any{}
	for _, ev := range hist {
		ent := ev["entidade"].(string)
		acao := ev["acao"].(string)
		byID[ent+"/"+acao] = ev
	}
	if ev := byID["marcacao/excluir"]; ev == nil {
		t.Fatalf("evento marcacao/excluir ausente: %v", hist)
	} else if ev["entidade_id"] != float64(marcID) {
		t.Fatalf("entidade_id errado na exclusão: %v", ev)
	}
	if ev := byID["marcacao/criar"]; ev == nil || ev["entidade_id"] != float64(marcID) {
		t.Fatalf("evento marcacao/criar ausente ou errado: %v", hist)
	}
	if ev := byID["artigo/criar"]; ev == nil {
		t.Fatalf("evento artigo/criar ausente: %v", hist)
	} else if dados, _ := ev["dados"].(map[string]any); dados == nil || dados["titulo"] != "Artigo Historico" {
		t.Fatalf("dados do artigo/criar errados: %v", ev)
	}
	if ev := byID["artigo/atualizar"]; ev == nil {
		t.Fatalf("evento artigo/atualizar ausente: %v", hist)
	}
	if hist[0]["acao"] != "excluir" || hist[0]["entidade"] != "marcacao" {
		t.Fatalf("ordem do histórico errada (deveria começar pela exclusão): %v", hist[0])
	}
	for _, ev := range hist {
		if ev["criado_em"] == nil || ev["criado_em"] == "" {
			t.Fatalf("evento sem criado_em: %v", ev)
		}
		if ev["dados"] == nil {
			t.Fatalf("evento sem dados: %v", ev)
		}
	}
}

func TestExportarDocxValido(t *testing.T) {
	ts, _ := newTestServer(t)
	criado := uploadPDF(t, ts, "Artigo Exportável Éxito")
	id := int64(criado["id"].(float64))

	postJSON(t, fmt.Sprintf("%s/api/artigos/%d/notas", ts.URL, id), map[string]any{
		"pagina": 1,
		"texto":  "nota para exportar",
	}).Body.Close()

	resp := do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/exportar", ts.URL, id), nil)
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("exportar: status %d", resp.StatusCode)
	}
	if out["nome"] != fmt.Sprintf("%d-artigo-exportavel-exito.docx", id) {
		t.Fatalf("nome do docx errado: %q", out["nome"])
	}
	if _, err := os.Stat(out["caminho"]); err != nil {
		t.Fatalf("docx não existe em disco: %v", err)
	}

	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/exportar", ts.URL, id), nil)
	docBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "wordprocessingml") {
		t.Fatalf("content-type errado no download: %s", ct)
	}
	if len(docBytes) < 4 || string(docBytes[:2]) != "PK" {
		t.Fatalf("download não é um zip/docx")
	}

	zr, err := zip.NewReader(bytes.NewReader(docBytes), int64(len(docBytes)))
	if err != nil {
		t.Fatalf("docx não abre como zip: %v", err)
	}
	names := map[string]bool{}
	var documentXML []byte
	for _, f := range zr.File {
		names[f.Name] = true
		if f.Name == "word/document.xml" {
			rc, _ := f.Open()
			documentXML, _ = io.ReadAll(rc)
			rc.Close()
		}
	}
	for _, e := range []string{"word/document.xml"} {
		if !names[e] {
			t.Errorf("entrada %q ausente no docx exportado", e)
		}
	}
	doc := string(documentXML)
	if strings.Count(doc, "<pic:pic>") != 0 {
		t.Errorf("conteúdo não pode ser exportado como imagem colada; encontrado <pic:pic>")
	}
	if !strings.Contains(doc, "nota para exportar") {
		t.Errorf("nota ausente do docx")
	}
	if !strings.Contains(doc, "Página 1: nota para exportar") {
		t.Errorf("âncora 'Página 1' ausente do docx")
	}
	if !strings.Contains(doc, "acentuação") {
		t.Errorf("texto com acento deveria estar no conteúdo editável")
	}
	if !strings.Contains(doc, "<w:t xml:space=\"preserve\">Olá mundo! Este é um artigo de teste.</w:t>") {
		t.Errorf("linha do PDF deveria estar como texto editável (w:t), não como imagem")
	}
	if !strings.Contains(doc, "<w:br w:type=\"page\"/>") {
		t.Errorf("quebra de página ausente entre páginas/notas")
	}
}

func TestNotaVinculadaAMarcacao(t *testing.T) {
	ts, _ := newTestServer(t)
	criado := uploadPDF(t, ts, "Artigo Vinculo")
	id := int64(criado["id"].(float64))

	resp := postJSON(t, fmt.Sprintf("%s/api/artigos/%d/marcacoes", ts.URL, id), map[string]any{
		"pagina": 1, "tipo": "highlight", "cor": "#FFEB3B",
		"palavras": [][]float64{{70, 87, 137, 102}}, "texto": "trecho",
	})
	var marc map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&marc)
	resp.Body.Close()
	marcID := int64(marc["id"].(float64))

	resp = postJSON(t, fmt.Sprintf("%s/api/artigos/%d/notas", ts.URL, id), map[string]any{
		"pagina": 1, "texto": "nota do trecho", "marcacao_id": marcID,
	})
	resp.Body.Close()

	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/notas", ts.URL, id), nil)
	var notas []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&notas)
	resp.Body.Close()
	if len(notas) != 1 || notas[0]["marcacao_id"] != float64(marcID) {
		t.Fatalf("nota sem vinculo: %v", notas)
	}

	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/historico", ts.URL, id), nil)
	var hist []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&hist)
	resp.Body.Close()
	for _, ev := range hist {
		if ev["entidade"] == "nota" && ev["acao"] == "criar" {
			if dados, _ := ev["dados"].(map[string]any); dados == nil || dados["marcacao_id"] != float64(marcID) {
				t.Fatalf("historico da nota sem marcacao_id: %v", ev)
			}
		}
	}
}

func TestStaticWWW(t *testing.T) {
	www := t.TempDir()
	if err := os.WriteFile(filepath.Join(www, "index.html"), []byte("<html>artigos ana</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(www, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(www, "assets", "x.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}

	a, err := New(t.TempDir(), testPopplerDir, testDSN, www)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	ts := httptest.NewServer(a.Routes())
	t.Cleanup(ts.Close)

	resp := do(t, http.MethodGet, ts.URL+"/", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "artigos ana") {
		t.Fatalf("GET / deveria servir o index.html: status %d, corpo %q", resp.StatusCode, body)
	}

	resp = do(t, http.MethodGet, ts.URL+"/assets/x.js", nil)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "console.log") {
		t.Fatalf("asset n�o servido: %q", body)
	}

	resp = do(t, http.MethodGet, ts.URL+"/alguma/rota/spa", nil)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "artigos ana") {
		t.Fatalf("fallback SPA n�o serviu o index: %q", body)
	}

	resp = do(t, http.MethodGet, ts.URL+"/api/health", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/health quebrou com www: status %d", resp.StatusCode)
	}
}
