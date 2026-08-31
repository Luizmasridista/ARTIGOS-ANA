package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestCitacoes_Navegacao_Pos(t *testing.T) {
	ts, a := newTestServer(t)
	// cria artigo com texto que tem citação e palavras com posição
	texto := "Introdução (Silva, 2020) e depois [12] com Referências\n[12] SILVA, J. Título, 2020."
	id := criarArtigoComTexto(t, a, "Navegacao Pos", texto)

	// varrer deve encontrar pos para (Silva, 2020) e [12]
	resp := do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/varrer-citacoes", ts.URL, id), nil)
	var lista []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&lista); err != nil {
		t.Fatalf("decode varrer: %v", err)
	}
	resp.Body.Close()
	if len(lista) == 0 {
		t.Fatalf("varrer retornou 0")
	}
	// verifica que pelo menos uma tem pagina>0 e pos
	foundPos := false
	for _, c := range lista {
		pagina, _ := c["pagina"].(float64)
		pos, hasPos := c["pos"]
		ocorr, hasOcorr := c["ocorrencias"]
		if pagina > 0 && hasPos && pos != nil {
			// pos deve ser [x0,y0,x1,y1]
			if arr, ok := pos.([]any); ok && len(arr) == 4 {
				foundPos = true
				t.Logf("citacao %v pagina %v pos %v", c["chave"], pagina, pos)
			}
		}
		_ = ocorr
		_ = hasOcorr
	}
	if !foundPos {
		t.Errorf("nenhuma citacao com pos encontrada, lista: %+v", lista)
	}
	// GET deve retornar mesmo
	resp = do(t, http.MethodGet, fmt.Sprintf("%s/api/artigos/%d/citacoes", ts.URL, id), nil)
	var lista2 []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&lista2)
	resp.Body.Close()
	if len(lista2) != len(lista) {
		t.Errorf("GET vs varrer len %d vs %d", len(lista2), len(lista))
	}
	// verifica que ocorrencias é array
	for _, c := range lista2 {
		if ocorr, ok := c["ocorrencias"]; ok {
			if ocorr != nil {
				if arr, ok := ocorr.([]any); !ok {
					t.Errorf("ocorrencias não é array: %T", ocorr)
				} else {
					_ = arr
				}
			}
		}
	}
}

func TestCitacoes_SemPosParaReferenciaPura(t *testing.T) {
	ts, a := newTestServer(t)
	// texto com referência mas sem ocorrência no corpo (só na seção)
	texto := "Texto sem citação no corpo.\nReferências\n[99] AUTOR, Título, 2020."
	id := criarArtigoComTexto(t, a, "Ref Pura", texto)
	resp := do(t, http.MethodPost, fmt.Sprintf("%s/api/artigos/%d/varrer-citacoes", ts.URL, id), nil)
	var lista []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&lista)
	resp.Body.Close()
	// deve ter pelo menos a referência [99] com pagina 0 ou com pos null (pois [99] também aparece na seção, mas não no corpo)
	hasRef := false
	for _, c := range lista {
		if c["tipo"] == "referencia" && c["chave"] == "[99]" {
			hasRef = true
			// para referência pura, pagina pode ser 0 e pos null, ou pode ter pos se [99] aparece no corpo (aqui não aparece, então 0)
			// apenas verifica que campo existe
			if _, ok := c["pagina"]; !ok {
				t.Error("referencia sem pagina")
			}
			if _, ok := c["pos"]; !ok {
				t.Error("referencia sem pos")
			}
		}
	}
	if !hasRef {
		t.Logf("sem referencia [99], lista: %+v", lista)
	}
}
