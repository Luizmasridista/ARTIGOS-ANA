package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVector_Enriquecer_DiretoVsUddg(t *testing.T) {
	ts, a := newTestServer(t)
	id := criarArtigoComTexto(t, a, "Vector Direto", "(Silva, 2020)")
	resp := do(t, "POST", ts.URL+"/api/artigos/"+fmt.Sprintf("%d", id)+"/varrer-citacoes", nil)
	var lista []map[string]any
	decodeJSON(t, resp.Body, &lista)
	resp.Body.Close()
	citID := int64(lista[0]["id"].(float64))

	mockDirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<a href="https://example.com/direct">x</a>`)
	}))
	defer mockDirect.Close()
	orig := duckDuckGoBaseURL
	duckDuckGoBaseURL = mockDirect.URL
	resp = do(t, "POST", ts.URL+"/api/artigos/"+fmt.Sprintf("%d", id)+"/citacoes/"+fmt.Sprintf("%d", citID)+"/enriquecer", nil)
	var out map[string]string
	decodeJSON(t, resp.Body, &out)
	resp.Body.Close()
	if out["url"] != "https://example.com/direct" {
		t.Fatalf("direto falhou: %q", out["url"])
	}
	duckDuckGoBaseURL = orig

	mockUddg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<a href="/l/?kh=-1&amp;uddg=https%3A%2F%2Fexample.com%2Fuddg">x</a>`)
	}))
	defer mockUddg.Close()
	duckDuckGoBaseURL = mockUddg.URL
	resp = do(t, "POST", ts.URL+"/api/artigos/"+fmt.Sprintf("%d", id)+"/citacoes/"+fmt.Sprintf("%d", citID)+"/enriquecer", nil)
	decodeJSON(t, resp.Body, &out)
	resp.Body.Close()
	if out["url"] != "https://example.com/uddg" {
		t.Fatalf("uddg falhou: %q", out["url"])
	}
	duckDuckGoBaseURL = orig

	mockFiltered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<a href="https://duckduckgo.com/about">x</a><a href="https://example.com/ok">y</a>`)
	}))
	defer mockFiltered.Close()
	duckDuckGoBaseURL = mockFiltered.URL
	resp = do(t, "POST", ts.URL+"/api/artigos/"+fmt.Sprintf("%d", id)+"/citacoes/"+fmt.Sprintf("%d", citID)+"/enriquecer", nil)
	decodeJSON(t, resp.Body, &out)
	resp.Body.Close()
	if out["url"] != "https://example.com/ok" {
		t.Fatalf("filtro duckduckgo falhou: %q", out["url"])
	}
	duckDuckGoBaseURL = orig
}

func TestVector_Enriquecer_RealVaswani(t *testing.T) {
	ts, a := newTestServer(t)
	id := criarArtigoComTexto(t, a, "Vector Real", "(Vaswani et al., 2017)")
	resp := do(t, "POST", ts.URL+"/api/artigos/"+fmt.Sprintf("%d", id)+"/varrer-citacoes", nil)
	var lista []map[string]any
	decodeJSON(t, resp.Body, &lista)
	resp.Body.Close()
	if len(lista) == 0 {
		t.Skip("sem citação para teste real")
	}
	citID := int64(lista[0]["id"].(float64))
	orig := duckDuckGoBaseURL
	duckDuckGoBaseURL = "https://html.duckduckgo.com/html/"
	resp = do(t, "POST", ts.URL+"/api/artigos/"+fmt.Sprintf("%d", id)+"/citacoes/"+fmt.Sprintf("%d", citID)+"/enriquecer", nil)
	var out map[string]string
	decodeJSON(t, resp.Body, &out)
	resp.Body.Close()
	duckDuckGoBaseURL = orig
	t.Logf("real Vaswani url: %q (vazio é ok se bloqueado, mas não deve ser 500)", out["url"])
	if resp.StatusCode != 200 {
		t.Fatalf("real enriquecer status %d", resp.StatusCode)
	}
}

func TestVector_Enriquecer_TiposDeCitacao(t *testing.T) {
	ts, a := newTestServer(t)
	casos := []struct {
		texto string
		tipo  string
	}{
		{"(Silva, 2020)", "autor_ano"},
		{"Silva (2020)", "autor_ano"},
		{"[12]", "numerica"},
		{"Referências\n[12] Autor, 2020", "referencia"},
	}
	for _, tc := range casos {
		id := criarArtigoComTexto(t, a, "Vector Tipo "+tc.tipo, tc.texto)
		resp := do(t, "POST", ts.URL+"/api/artigos/"+fmt.Sprintf("%d", id)+"/varrer-citacoes", nil)
		var lista []map[string]any
		decodeJSON(t, resp.Body, &lista)
		resp.Body.Close()
		found := false
		for _, c := range lista {
			if c["tipo"] == tc.tipo {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tipo %s não encontrado para texto %q, lista %v", tc.tipo, tc.texto, lista)
		}
		if len(lista) > 0 {
			citID := int64(lista[0]["id"].(float64))
			mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, `<a href="https://example.com/`+tc.tipo+`">x</a>`)
			}))
			orig := duckDuckGoBaseURL
			duckDuckGoBaseURL = mock.URL
			resp = do(t, "POST", ts.URL+"/api/artigos/"+fmt.Sprintf("%d", id)+"/citacoes/"+fmt.Sprintf("%d", citID)+"/enriquecer", nil)
			var out map[string]string
			decodeJSON(t, resp.Body, &out)
			resp.Body.Close()
			mock.Close()
			duckDuckGoBaseURL = orig
			if out["url"] != "https://example.com/"+tc.tipo {
				t.Errorf("enriquecer tipo %s falhou: %q", tc.tipo, out["url"])
			}
		}
	}
}

func TestVector_Enriquecer_QVariados(t *testing.T) {
	ts, a := newTestServer(t)
	casos := []string{
		"Silva",
		strings.Repeat("A", 5000) + " (Silva, 2020)",
		"😀 Vaswani et al. 2017",
		"'; DROP TABLE citacoes; --",
		"<script>alert(1)</script>",
	}
	for i, q := range casos {
		id := criarArtigoComTexto(t, a, "Vector Q", q)
		resp := do(t, "POST", ts.URL+"/api/artigos/"+fmt.Sprintf("%d", id)+"/varrer-citacoes", nil)
		var lista []map[string]any
		decodeJSON(t, resp.Body, &lista)
		resp.Body.Close()
		if len(lista) == 0 {
			continue
		}
		citID := int64(lista[0]["id"].(float64))
		iCopy := i
		mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			if r.FormValue("q") == "" {
				t.Errorf("caso %d q vazio no mock", iCopy)
			}
			io.WriteString(w, `<a href="https://example.com/q`+fmt.Sprintf("%d", iCopy)+`">x</a>`)
		}))
		orig := duckDuckGoBaseURL
		duckDuckGoBaseURL = mock.URL
		resp = do(t, "POST", ts.URL+"/api/artigos/"+fmt.Sprintf("%d", id)+"/citacoes/"+fmt.Sprintf("%d", citID)+"/enriquecer", nil)
		var out map[string]string
		decodeJSON(t, resp.Body, &out)
		resp.Body.Close()
		mock.Close()
		duckDuckGoBaseURL = orig
		if out["url"] != "https://example.com/q"+fmt.Sprintf("%d", i) {
			t.Errorf("caso %d q %q falhou: %q", i, q[:20], out["url"])
		}
	}
}

func decodeJSON(t *testing.T, r io.Reader, v any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(v); err != nil {
		t.Fatalf("decode json: %v", err)
	}
}
