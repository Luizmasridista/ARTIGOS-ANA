package app

import (
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"artigos-ana/backend/internal/pdf"
	"artigos-ana/backend/internal/store"
)

type sumarioItem struct {
	Titulo string `json:"titulo"`
	Pagina int    `json:"pagina"`
	Nivel  int    `json:"nivel"`
	Ordem  int    `json:"ordem"`
}

var (
	reNumTitulo    = regexp.MustCompile(`^\d+(\.\d+)*\.?\s+[A-ZÁÂÃÉÊÍÓÔÕÇ]`)
	rePureNumber   = regexp.MustCompile(`^\d+(\.\d+)?%?$`)
	commonKeywords = []string{
		"introdução", "introducao",
		"metodologia", "métodos", "metodos",
		"resultados", "resultado",
		"discussão", "discussao",
		"conclusão", "conclusao", "considerações", "consideracoes",
		"referências", "referencias", "bibliografia",
		"resumo", "abstract",
		"agradecimentos", "anexo", "apêndice", "apendice",
		"fundamentação", "revisão", "revisao",
		// English fallback
		"introduction", "methodology", "methods", "results", "discussion", "conclusion", "references", "background",
		"architecture", "architectures", "infrastructure", "infrastructures", "training", "pre-training", "post-training", "evaluation", "experiment", "experiments", "limitation", "limitations", "future",
	}
)

func (a *App) handleSumario(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	paginas, err := a.DB.ListPaginas(id)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao buscar páginas")
		return
	}
	items := gerarSumario(paginas)
	if items == nil {
		items = []sumarioItem{}
	}
	writeJSON(w, http.StatusOK, items)
}

type linhaInfo struct {
	Texto string
	Y0    float64
	X0    float64
	X1    float64
	Y1    float64
}

func gerarSumario(paginas []store.Pagina) []sumarioItem {
	var out []sumarioItem
	ordem := 1
	for _, p := range paginas {
		var words []pdf.Word
		if err := json.Unmarshal(p.CamadaJSON, &words); err != nil || len(words) == 0 {
			continue
		}
		linhas := agruparLinhas(words, p.LarguraPx)
		for _, l := range linhas {
			txt := strings.TrimSpace(l.Texto)
			if txt == "" {
				continue
			}
			if len([]rune(txt)) >= 80 || len([]rune(txt)) < 3 {
				continue
			}
			if ehTitulo(l, p.LarguraPx) {
				// limpa numeração duplicada? mantém original
				clean := strings.TrimSpace(txt)
				// limita tamanho do título
				if len([]rune(clean)) > 80 {
					clean = string([]rune(clean)[:80])
				}
				out = append(out, sumarioItem{
					Titulo: clean,
					Pagina: p.Numero,
					Nivel:  1,
					Ordem:  ordem,
				})
				ordem++
			}
		}
	}
	return out
}

func agruparLinhas(words []pdf.Word, larguraPx int) []linhaInfo {
	var filtered []pdf.Word
	for _, w := range words {
		if strings.TrimSpace(w.Texto) == "" {
			continue
		}
		filtered = append(filtered, w)
	}
	if len(filtered) == 0 {
		return nil
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if math.Abs(filtered[i].Y0-filtered[j].Y0) > 4 {
			return filtered[i].Y0 < filtered[j].Y0
		}
		return filtered[i].X0 < filtered[j].X0
	})
	var linhas []linhaInfo
	var cur []pdf.Word
	var curY float64
	flush := func() {
		if len(cur) == 0 {
			return
		}
		sort.Slice(cur, func(i, j int) bool { return cur[i].X0 < cur[j].X0 })
		var parts []string
		for _, w := range cur {
			parts = append(parts, w.Texto)
		}
		texto := strings.Join(parts, " ")
		texto = strings.Join(strings.Fields(texto), " ")
		if texto == "" {
			cur = nil
			return
		}
		minX0 := cur[0].X0
		minY0 := cur[0].Y0
		maxX1 := cur[0].X1
		maxY1 := cur[0].Y1
		for _, w := range cur[1:] {
			if w.X0 < minX0 {
				minX0 = w.X0
			}
			if w.Y0 < minY0 {
				minY0 = w.Y0
			}
			if w.X1 > maxX1 {
				maxX1 = w.X1
			}
			if w.Y1 > maxY1 {
				maxY1 = w.Y1
			}
		}
		linhas = append(linhas, linhaInfo{Texto: texto, Y0: minY0, X0: minX0, X1: maxX1, Y1: maxY1})
		cur = nil
	}
	for _, w := range filtered {
		if len(cur) == 0 {
			cur = append(cur, w)
			curY = w.Y0
			continue
		}
		altura := math.Max(w.Y1-w.Y0, 1)
		if math.Abs(w.Y0-curY) < altura*0.6 {
			// gap grande indica quebra de linha (figura)
			if len(cur) > 0 {
				last := cur[len(cur)-1]
				gap := w.X0 - last.X1
				if gap > 80 {
					flush()
					cur = append(cur, w)
					curY = w.Y0
					continue
				}
			}
			cur = append(cur, w)
		} else {
			flush()
			cur = append(cur, w)
			curY = w.Y0
		}
	}
	flush()
	sort.Slice(linhas, func(i, j int) bool { return linhas[i].Y0 < linhas[j].Y0 })
	return linhas
}

func ehTitulo(l linhaInfo, larguraPx int) bool {
	txt := strings.TrimSpace(l.Texto)
	if txt == "" {
		return false
	}
	if len([]rune(txt)) >= 80 || len([]rune(txt)) < 3 {
		return false
	}
	if strings.Contains(txt, "%") || strings.Contains(txt, "=") || strings.Contains(txt, "∈") || strings.Contains(txt, "×") || strings.Contains(txt, "∑") {
		return false
	}
	if strings.Contains(txt, " / ") && isUpperLine(txt) && len(strings.Fields(txt)) <= 3 {
		return false
	}
	for _, r := range txt {
		if r > 0xFF {
			if r != '—' && r != '–' && r != '…' && r != '’' && r != '‘' && r != '“' && r != '”' {
				return false
			}
		}
	}
	// numerado
	if reNumTitulo.MatchString(txt) {
		numPart := strings.Fields(txt)[0]
		numStr := strings.Split(numPart, ".")[0]
		if n, err := parsePositiveInt(numStr); err == nil {
			if n == 0 || n > 20 {
				return false
			}
			if n > 6 {
				// para números grandes (figura), só aceita se for título comum
				locTmp := reNumTitulo.FindStringIndex(txt)
				sufTmp := ""
				if locTmp != nil && locTmp[1] < len(txt) {
					sufTmp = strings.TrimSpace(txt[locTmp[1]-1:])
				}
				if !isCommonTitle(sufTmp) && !isCommonTitleContains(sufTmp) {
					return false
				}
			}
		}
		loc := reNumTitulo.FindStringIndex(txt)
		suffix := ""
		if loc != nil && loc[1] < len(txt) {
			suffix = strings.TrimSpace(txt[loc[1]-1:])
		} else {
			suffix = txt
		}
		fields := strings.Fields(suffix)
		if len(fields) > 1 && rePureNumber.MatchString(fields[len(fields)-1]) {
			fields = fields[:len(fields)-1]
			suffix = strings.Join(fields, " ")
		}
		midFields := strings.Fields(suffix)
		for i, f := range midFields {
			if i == 0 {
				continue
			}
			if rePureNumber.MatchString(f) {
				return false
			}
		}
		if isCommonTitle(suffix) || isCommonTitleContains(suffix) {
			return true
		}
		if isFirstUpper(suffix) && len(strings.Fields(suffix)) <= 6 {
			return true
		}
		if isCentralizado(l, larguraPx) {
			return true
		}
		return false
	}
	if isUpperLine(txt) {
		if isCentralizado(l, larguraPx) && (isCommonTitle(txt) || isCommonTitleContains(txt)) {
			if len(strings.Fields(txt)) <= 8 {
				return true
			}
		}
	}
	if isCommonTitle(txt) && isFirstUpper(txt) {
		wc := len(strings.Fields(txt))
		if wc <= 8 {
			return true
		}
	}
	return false
}

func isUpperLine(s string) bool {
	trim := strings.TrimSpace(s)
	if trim == "" {
		return false
	}
	upper := strings.ToUpper(trim)
	lower := strings.ToLower(trim)
	if lower == upper {
		return false
	}
	return upper == trim
}

func isCommonTitle(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	for _, kw := range commonKeywords {
		if lower == kw || strings.HasPrefix(lower, kw+" ") || strings.HasPrefix(lower, kw+":") || strings.HasPrefix(lower, kw+".") {
			return true
		}
		if strings.HasPrefix(lower, kw+" -") || strings.HasPrefix(lower, kw+" —") {
			return true
		}
	}
	return false
}

func isCommonTitleContains(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	for _, kw := range commonKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func isCentralizado(l linhaInfo, larguraPx int) bool {
	if larguraPx <= 0 {
		return false
	}
	pageCenter := float64(larguraPx) / 2
	lineCenter := (l.X0 + l.X1) / 2
	dist := math.Abs(lineCenter - pageCenter)
	return dist < float64(larguraPx)*0.20
}

func parsePositiveInt(s string) (int, error) {
	s = strings.Trim(s, ".")
	return strconv.Atoi(s)
}

func isFirstUpper(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			return true
		}
		if r >= 'À' && r <= 'Þ' && r != '×' {
			return true
		}
		if (r >= 'a' && r <= 'z') || (r >= 'à' && r <= 'ÿ') {
			return false
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '.' || r == ' ' {
			continue
		}
		return false
	}
	return false
}
