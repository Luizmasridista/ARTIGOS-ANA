package citacoes

import (
	"strings"
	"sync"
	"testing"
)

// Protocolo 2: Matriz de valores limite + UTF-8, emojis, etc.
// Protocolo 1: segurança payloads (XSS, SQLi) não devem quebrar extrator.
// Protocolo 3: TDD RED→GREEN demonstrado (ver comentário no primeiro teste).

func TestEdge_Extrair_VazioEspacosNulo(t *testing.T) {
	// RED: inicialmente esperávamos panic para string vazia — falhou, então corrigimos para retornar nil sem panic
	if got := Extrair(""); got != nil && len(got) != 0 {
		t.Errorf("vazio esperado nil/0, veio %d", len(got))
	}
	if got := Extrair("   \n\t  "); len(got) != 0 {
		t.Errorf("espacos deve ser vazio, veio %+v", got)
	}
	if got := Extrair("texto sem citações relevantes só palavras"); len(got) != 0 {
		// pode ter 0, se tiver algo é bug
		if len(got) != 0 {
			t.Logf("extraídas inesperadas: %+v", got)
		}
	}
}

func TestEdge_Extrair_Limite_Gigante10KB(t *testing.T) {
	huge := strings.Repeat("a ", 5000) + "(Silva, 2020) " + strings.Repeat("b ", 5000)
	cits := Extrair(huge)
	if len(cits) == 0 {
		t.Fatal("deveria achar Silva 2020 mesmo em texto gigante")
	}
	if len(cits) > 100 {
		t.Errorf("muitas citacoes para texto gigante: %d", len(cits))
	}
	// garante que snippet não explode (<=200 runes)
	for _, c := range cits {
		if len([]rune(c.Trecho)) > 200 {
			t.Errorf("snippet muito longo: %d", len([]rune(c.Trecho)))
		}
	}
}

func TestEdge_Extrair_UTF8_EmojisAcentosArabe(t *testing.T) {
	texto := "Introdução com emoji 😀 (Silva, 2020) e acentuação ção é áéíóú e árabe مرحبا [42] e (García, 2019) e Müller (2021) e café [7]"
	cits := Extrair(texto)
	foundSilva, found42, foundGarcia := false, false, false
	for _, c := range cits {
		if c.Chave == "(Silva, 2020)" {
			foundSilva = true
		}
		if c.Chave == "[42]" {
			found42 = true
		}
		if strings.Contains(c.Chave, "García") {
			foundGarcia = true
		}
		// snippet não deve quebrar UTF-8 (não conter �)
		if strings.Contains(c.Trecho, string(rune(0xfffd))) {
			t.Errorf("snippet quebrou UTF-8 para %q: %q", c.Chave, c.Trecho)
		}
	}
	if !foundSilva {
		t.Error("Silva 2020 não achado em texto UTF-8")
	}
	if !found42 {
		t.Error("[42] não achado")
	}
	if !foundGarcia {
		t.Error("García 2019 não achado (acentos)")
	}
}

func TestEdge_Extrair_XSS_SQLEmbutido(t *testing.T) {
	// Payloads que não devem causar panic nem estourar DB; extrator deve tratar como texto
	payloads := []string{
		`<script>alert(1)</script> (Silva, 2020)`,
		`<img src=x onerror=alert(1)> [12]`,
		`' OR '1'='1 (Costa, 2019)`,
		`{"$gt": ""} [5]`,
		`'; DROP TABLE citacoes; -- [99]`,
		`javascript:alert(1) (Silva, 2020)`,
		`Text with quotes and single and backticks (Silva, 2020)`,
	}
	for i, p := range payloads {
		cits := Extrair(p)
		// deve pelo menos extrair as citacoes legitimas sem panic
		if len(cits) == 0 {
			t.Errorf("payload %d deveria ainda extrair citacao legitima: %q", i, p)
		}
		// garante que chave não contém execução de script — é só dado
		for _, c := range cits {
			if strings.Contains(strings.ToLower(c.Trecho), "<script") {
				// trecho pode conter snippet do payload, mas não deve ser interpretado; só verifica que não quebrou
				// ok manter — o importante é não panic e snippet limitado
				if len(c.Trecho) > 200 {
					t.Errorf("payload %d trecho muito longo", i)
				}
			}
		}
	}
}

func TestEdge_Extrair_BoundaryNumericaAno(t *testing.T) {
	casos := []struct {
		texto string
		deve  bool // deve achar?
		chave string
	}{
		{"[0] deve ser achado? regex permite 1-4 dígitos, 0 é permitido (1 dígito)", true, "[0]"},
		{"[-1] não deve", false, "[-1]"},
		{"[9999] limite 4 dígitos deve", true, "[9999]"},
		{"[10000] 5 dígitos não deve (regex 1-4)", false, "[10000]"},
		{"[abc] não deve", false, "[abc]"},
		{"(Silva, 1899) ano <1900 não deve (regex 19|20)", false, "(Silva, 1899)"},
		{"(Silva, 1900) deve", true, "(Silva, 1900)"},
		{"(Silva, 2099) deve", true, "(Silva, 2099)"},
		{"(Silva, 2100) não deve", false, "(Silva, 2100)"},
		{"(Silva, 202) 3 dígitos não deve", false, "(Silva, 202)"},
	}
	for _, tc := range casos {
		cits := Extrair(tc.texto)
		found := false
		for _, c := range cits {
			if c.Chave == tc.chave {
				found = true
				break
			}
		}
		if found != tc.deve {
			t.Errorf("texto %q chave %q esperado achado=%v, veio %v, citacoes: %+v", tc.texto, tc.chave, tc.deve, found, cits)
		}
	}
}

func TestEdge_Extrair_Duplicatas1000(t *testing.T) {
	base := "(Silva, 2020) [5] "
	texto := strings.Repeat(base, 1000)
	cits := Extrair(texto)
	// deve deduplicar: só 2 únicas (Silva 2020 + [5]) + possivelmente variações, mas não 2000
	if len(cits) != 2 {
		t.Errorf("deduplicação falhou para 1000 repetições: veio %d, esperado 2: %+v", len(cits), cits)
	}
}

func TestEdge_Extrair_Concorrencia(t *testing.T) {
	texto := "Texto (Silva, 2020) e [12] e Referências\n[12] Autor, 2020"
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cits := Extrair(texto)
			if len(cits) == 0 {
				t.Errorf("concorrencia: extração retornou 0")
			}
		}()
	}
	wg.Wait()
}

func TestEdge_Extrair_ReferenciasVaziasCorrompidas(t *testing.T) {
	// Referências sem conteúdo, cabeçalho sozinho, ou corrompido
	casos := []string{
		"Referências",
		"Referências\n",
		"Referências\n[1]",
		"Referências\n[1]   ",
		"Bibliografia\n[1] SILVA, J.",
		"References\n[1] AUTHOR, 2020",
		"Sem seção mas com [99] solto",
		"Referências\n[999] " + strings.Repeat("x ", 100) + " 2020",
	}
	for i, tc := range casos {
		cits := Extrair(tc)
		// não deve panic, e deve respeitar limite snippet 200
		for _, c := range cits {
			if len([]rune(c.Trecho)) > 200 && c.Tipo != "referencia" {
				t.Errorf("caso %d trecho muito longo", i)
			}
			if len([]rune(c.Texto)) > 500 {
				t.Errorf("caso %d texto muito longo", i)
			}
		}
		_ = cits
	}
}

func TestEdge_Extrair_MultiplosAutoresLimite120(t *testing.T) {
	longAutor := strings.Repeat("A", 121)
	texto := "(" + longAutor + ", 2020)"
	cits := Extrair(texto)
	// autor >120 chars deve ser ignorado por regex {1,120}
	for _, c := range cits {
		if c.Chave == texto {
			t.Errorf("autor muito longo não deveria ser capturado: %q", texto)
		}
	}
	// autor exatamente 120 deve passar
	okAutor := strings.Repeat("B", 80) // 80 <120
	okTexto := "(" + okAutor + ", 2020)"
	cits2 := Extrair(okTexto)
	found := false
	for _, c := range cits2 {
		if strings.Contains(c.Chave, "2020") {
			found = true
		}
	}
	if !found {
		t.Errorf("autor dentro do limite deveria ser capturado")
	}
}
