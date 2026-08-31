package app

import (
	"encoding/json"
	"net/http"
	"strings"

	"artigos-ana/backend/internal/pdf"
	"artigos-ana/backend/internal/store"
)

type buscaResultado struct {
	Pagina int        `json:"pagina"`
	Pos    [4]float64 `json:"pos"`
	Trecho string     `json:"trecho"`
}

func (a *App) handleBusca(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	runeLen := len([]rune(q))
	if runeLen < 2 || runeLen > 100 {
		writeErro(w, http.StatusBadRequest, "q deve ter entre 2 e 100 caracteres")
		return
	}
	if len(q) > 10*1024 {
		writeErro(w, http.StatusBadRequest, "q muito longo")
		return
	}
	paginas, err := a.DB.ListPaginas(id)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao buscar páginas")
		return
	}
	results := buscarNasPaginas(paginas, q)
	if results == nil {
		results = []buscaResultado{}
	}
	writeJSON(w, http.StatusOK, results)
}

func buscarNasPaginas(paginas []store.Pagina, q string) []buscaResultado {
	qLowerRunes := []rune(strings.ToLower(q))
	if len(qLowerRunes) == 0 {
		return nil
	}
	var out []buscaResultado
	for _, p := range paginas {
		var words []pdf.Word
		if err := json.Unmarshal(p.CamadaJSON, &words); err != nil || len(words) == 0 {
			continue
		}
		// filtra palavras vazias
		var filtered []pdf.Word
		var texts []string
		var runeLens []int
		for _, w := range words {
			if strings.TrimSpace(w.Texto) == "" {
				continue
			}
			filtered = append(filtered, w)
			texts = append(texts, w.Texto)
			runeLens = append(runeLens, len([]rune(w.Texto)))
		}
		if len(filtered) == 0 {
			continue
		}
		joined := strings.Join(texts, " ")
		joinedRunes := []rune(joined)
		lowerJoined := strings.ToLower(joined)
		lowerRunes := []rune(lowerJoined)
		// starts em runes
		starts := make([]int, len(filtered))
		off := 0
		for i, t := range texts {
			starts[i] = off
			off += len([]rune(t)) + 1
		}
		// busca todas ocorrências
		searchFrom := 0
		for searchFrom+len(qLowerRunes) <= len(lowerRunes) {
			idx := indexRunes(lowerRunes[searchFrom:], qLowerRunes)
			if idx == -1 {
				break
			}
			abs := searchFrom + idx
			end := abs + len(qLowerRunes)
			// trecho 30 antes/depois
			lo := abs - 30
			if lo < 0 {
				lo = 0
			}
			hi := end + 30
			if hi > len(joinedRunes) {
				hi = len(joinedRunes)
			}
			trecho := string(joinedRunes[lo:hi])
			trecho = strings.ReplaceAll(trecho, "\n", " ")
			trecho = strings.Join(strings.Fields(trecho), " ")
			if len([]rune(trecho)) > 120 {
				trecho = string([]rune(trecho)[:120])
			}
			// mapear palavras sobrepostas
			startWord := -1
			endWord := -1
			for wi, ws := range starts {
				we := ws + runeLens[wi]
				if ws < end && we > abs {
					if startWord == -1 {
						startWord = wi
					}
					endWord = wi
				} else if ws >= abs && ws < end {
					if startWord == -1 {
						startWord = wi
					}
					endWord = wi
				} else if we > abs && we <= end {
					if startWord == -1 {
						startWord = wi
					}
					endWord = wi
				}
			}
			if startWord == -1 {
				for wi, ws := range starts {
					if ws <= abs {
						startWord = wi
					}
				}
				if startWord == -1 {
					startWord = 0
				}
				endWord = startWord
			}
			if endWord == -1 || endWord < startWord {
				endWord = startWord
			}
			if startWord < 0 {
				startWord = 0
			}
			if endWord >= len(filtered) {
				endWord = len(filtered) - 1
			}
			if startWord >= len(filtered) {
				startWord = len(filtered) - 1
			}
			minX0 := filtered[startWord].X0
			minY0 := filtered[startWord].Y0
			maxX1 := filtered[startWord].X1
			maxY1 := filtered[startWord].Y1
			for wi := startWord + 1; wi <= endWord; wi++ {
				if filtered[wi].X0 < minX0 {
					minX0 = filtered[wi].X0
				}
				if filtered[wi].Y0 < minY0 {
					minY0 = filtered[wi].Y0
				}
				if filtered[wi].X1 > maxX1 {
					maxX1 = filtered[wi].X1
				}
				if filtered[wi].Y1 > maxY1 {
					maxY1 = filtered[wi].Y1
				}
			}
			if maxX1 <= minX0 || maxY1 <= minY0 {
				searchFrom = abs + 1
				continue
			}
			out = append(out, buscaResultado{
				Pagina: p.Numero,
				Pos:    [4]float64{minX0, minY0, maxX1, maxY1},
				Trecho: trecho,
			})
			if len(out) >= 100 {
				return out
			}
			searchFrom = abs + 1
			if searchFrom >= len(lowerRunes) {
				break
			}
		}
	}
	return out
}

func indexRunes(haystack, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	if len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
