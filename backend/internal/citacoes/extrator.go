package citacoes

import (
	"regexp"
	"strconv"
	"strings"
)

// CitacaoExtraida representa uma citação encontrada no texto.
type CitacaoExtraida struct {
	Tipo        string       // autor_ano | numerica | referencia
	Chave       string       // string crua: "(Silva, 2020)" ou "[12]" ou "[1]"
	Autor       string       // quando detectável
	Ano         *int         // quando detectável
	Trecho      string       // snippet ao redor da citação (para autor_ano/numerica)
	Titulo      string       // para referencia: título/entrada completa
	Texto       string       // para referencia: texto completo da entrada; para demais: igual a Trecho ou vazio
	Pagina      int          // página da primeira ocorrência (0 se sem)
	Pos         *[4]float64  // bbox [x0,y0,x1,y1] na escala da camada, nil se sem
	Ocorrencias []Ocorrencia // todas as ocorrências encontradas
}

type Ocorrencia struct {
	Pagina int         `json:"pagina"`
	Pos    *[4]float64 `json:"pos"`
	Trecho string      `json:"trecho"`
}

type WordPos struct {
	Texto string
	X0    float64
	Y0    float64
	X1    float64
	Y1    float64
}

type PaginaComPalavras struct {
	Numero   int
	Palavras []WordPos
	Texto    string // cache do texto joinado
}

// Regex stdlib apenas.
var (
	reNumerica = regexp.MustCompile(`\[(\d{1,4})\]`)
	// (Autor, 2020) ; (Silva et al., 2020) ; (Silva & Costa, 2020) ; (Silva e Costa, 2020)
	// Captura: grupo1 = parte do autor (ex: "Silva et al."), grupo2 = ano
	reParenComma = regexp.MustCompile(`\(\s*([^()]{1,120}?)\s*,\s*(19\d{2}|20\d{2})\s*\)`)

	// Autor (2020) ; Silva et al. (2020)  -> autor fora dos parênteses
	reAutorSimples = regexp.MustCompile(`\b([A-ZÀ-Ü][A-Za-zÀ-ü]+(?:\s+et al\.?)?)\s*\(\s*(19\d{2}|20\d{2})\s*\)`)
	// Silva & Costa (2020) / Silva e Costa (2020)
	reAutorAmp = regexp.MustCompile(`\b([A-ZÀ-Ü][A-Za-zÀ-ü]+\s+(?:&|e)\s+[A-ZÀ-Ü][A-Za-zÀ-ü]+)\s*\(\s*(19\d{2}|20\d{2})\s*\)`)

	// Cabeçalho de referências (case-insensitive, encontra palavra em qualquer lugar)
	reRefHeader = regexp.MustCompile(`(?i)\b(refer[eê]ncias|bibliografia|references)\b`)
	// Linha de referência numerada: [12] texto...
	reRefLinha   = regexp.MustCompile(`^\s*\[(\d+)\]\s*(.+)$`)
	reRefBracket = regexp.MustCompile(`\[(\d+)\]`)
)

// Extrair varre o texto completo e retorna citações deduplicadas.
func Extrair(texto string) []CitacaoExtraida {
	if strings.TrimSpace(texto) == "" {
		return nil
	}
	var out []CitacaoExtraida
	seen := map[string]bool{}

	add := func(c CitacaoExtraida) {
		if c.Chave == "" {
			return
		}
		key := c.Tipo + "|" + c.Chave
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, c)
	}

	// 1) Numéricas [n] em todo o texto
	for _, loc := range reNumerica.FindAllStringSubmatchIndex(texto, -1) {
		if len(loc) < 4 {
			continue
		}
		s, e := loc[0], loc[1]
		chave := texto[s:e]
		// Evita falsos positivos muito longos? já limitado
		trecho := snippet(texto, s, e)
		add(CitacaoExtraida{
			Tipo:   "numerica",
			Chave:  chave,
			Trecho: trecho,
			Texto:  trecho,
		})
	}

	// 2) Parênteses com vírgula: (Autor, Ano)
	for _, loc := range reParenComma.FindAllStringSubmatchIndex(texto, -1) {
		if len(loc) < 6 {
			continue
		}
		s, e := loc[0], loc[1]
		gs1, ge1 := loc[2], loc[3]
		gs2, ge2 := loc[4], loc[5]
		if gs1 < 0 || gs2 < 0 {
			continue
		}
		autorRaw := strings.TrimSpace(texto[gs1:ge1])
		anoStr := texto[gs2:ge2]
		// Validação mínima do autor: deve conter letra e começar com maiúscula ou letra
		if autorRaw == "" {
			continue
		}
		// Limpar quebras internas
		autorRaw = strings.Join(strings.Fields(autorRaw), " ")
		// Se autorRaw contém apenas números/pontuação, ignora
		if !containsLetter(autorRaw) {
			continue
		}
		ano, err := strconv.Atoi(anoStr)
		if err != nil {
			continue
		}
		chave := texto[s:e]
		trecho := snippet(texto, s, e)
		// Normaliza autor: remove sufixos? mantém como está
		add(CitacaoExtraida{
			Tipo:   "autor_ano",
			Chave:  chave,
			Autor:  autorRaw,
			Ano:    &ano,
			Trecho: trecho,
			Texto:  trecho,
		})
	}

	// 3) Autor (Ano) simples
	for _, loc := range reAutorSimples.FindAllStringSubmatchIndex(texto, -1) {
		if len(loc) < 6 {
			continue
		}
		s, e := loc[0], loc[1]
		gs1, ge1 := loc[2], loc[3]
		gs2, ge2 := loc[4], loc[5]
		if gs1 < 0 || gs2 < 0 {
			continue
		}
		autorRaw := strings.TrimSpace(texto[gs1:ge1])
		anoStr := texto[gs2:ge2]
		autorRaw = strings.Join(strings.Fields(autorRaw), " ")
		if autorRaw == "" || !containsLetter(autorRaw) {
			continue
		}
		ano, _ := strconv.Atoi(anoStr)
		chave := texto[s:e]
		trecho := snippet(texto, s, e)
		add(CitacaoExtraida{
			Tipo:   "autor_ano",
			Chave:  chave,
			Autor:  autorRaw,
			Ano:    &ano,
			Trecho: trecho,
			Texto:  trecho,
		})
	}

	// 4) Autor &/e Autor (Ano)
	for _, loc := range reAutorAmp.FindAllStringSubmatchIndex(texto, -1) {
		if len(loc) < 6 {
			continue
		}
		s, e := loc[0], loc[1]
		gs1, ge1 := loc[2], loc[3]
		gs2, ge2 := loc[4], loc[5]
		if gs1 < 0 || gs2 < 0 {
			continue
		}
		autorRaw := strings.TrimSpace(texto[gs1:ge1])
		anoStr := texto[gs2:ge2]
		autorRaw = strings.Join(strings.Fields(autorRaw), " ")
		if autorRaw == "" {
			continue
		}
		ano, _ := strconv.Atoi(anoStr)
		chave := texto[s:e]
		trecho := snippet(texto, s, e)
		// Evita duplicar já capturado pelo simples (ex: Silva et al não entra aqui)
		add(CitacaoExtraida{
			Tipo:   "autor_ano",
			Chave:  chave,
			Autor:  autorRaw,
			Ano:    &ano,
			Trecho: trecho,
			Texto:  trecho,
		})
	}

	// 5) Seção de referências: extrai entradas [n] ...
	refSec := extrairSecaoReferencias(texto)
	if refSec != "" {
		// Encontra todas as ocorrências de [n] na seção de referências
		brackets := reRefBracket.FindAllStringSubmatchIndex(refSec, -1)
		for i, loc := range brackets {
			if len(loc) < 4 {
				continue
			}
			num := refSec[loc[2]:loc[3]]
			// conteúdo: do fim do bracket até o próximo bracket (ou fim)
			start := loc[1]
			end := len(refSec)
			if i+1 < len(brackets) {
				end = brackets[i+1][0]
			}
			conteudo := strings.TrimSpace(refSec[start:end])
			// Limpa quebras e múltiplos espaços
			conteudo = strings.ReplaceAll(conteudo, "\n", " ")
			conteudo = strings.ReplaceAll(conteudo, "\r", " ")
			conteudo = strings.Join(strings.Fields(conteudo), " ")
			if conteudo == "" {
				continue
			}
			chave := "[" + num + "]"
			linTrim := chave + " " + conteudo
			// Tenta extrair autor/ano da entrada
			var autorRef string
			var anoRef *int
			if anoMatch := regexp.MustCompile(`(19\d{2}|20\d{2})`).FindString(conteudo); anoMatch != "" {
				if v, err := strconv.Atoi(anoMatch); err == nil {
					anoRef = &v
				}
				parts := strings.Split(conteudo, ",")
				if len(parts) > 0 {
					cand := strings.TrimSpace(parts[0])
					if cand != "" && containsLetter(cand) && len(cand) < 80 {
						autorRef = cand
					}
				}
			} else {
				parts := strings.Split(conteudo, ",")
				if len(parts) > 0 {
					cand := strings.TrimSpace(parts[0])
					if cand != "" && len(cand) < 80 {
						autorRef = cand
					}
				}
			}
			add(CitacaoExtraida{
				Tipo:   "referencia",
				Chave:  chave,
				Autor:  autorRef,
				Ano:    anoRef,
				Trecho: conteudo,
				Titulo: conteudo,
				Texto:  linTrim,
			})
		}
	}

	return out
}

func ExtrairComPosicao(texto string, paginas []PaginaComPalavras) []CitacaoExtraida {
	base := Extrair(texto)
	if len(paginas) == 0 {
		return base
	}
	// Prepara cache de texto joinado por página (lower)
	type pgCache struct {
		num        int
		lower      string
		words      []WordPos
		joined     string
		wordStarts []int // byte index de cada palavra no joined lower
	}
	var caches []pgCache
	for _, pg := range paginas {
		if len(pg.Palavras) == 0 {
			continue
		}
		words := make([]string, len(pg.Palavras))
		for i, w := range pg.Palavras {
			words[i] = w.Texto
		}
		joined := strings.Join(words, " ")
		lower := strings.ToLower(joined)
		starts := make([]int, len(words))
		off := 0
		for i, w := range words {
			starts[i] = off
			off += len(w) + 1 // +1 espaço, último também mas não importa
		}
		caches = append(caches, pgCache{num: pg.Numero, lower: lower, words: pg.Palavras, joined: joined, wordStarts: starts})
	}
	for i := range base {
		chaveLower := strings.ToLower(strings.TrimSpace(base[i].Chave))
		if chaveLower == "" {
			continue
		}
		var occs []Ocorrencia
		for _, pc := range caches {
			lowerChave := chaveLower
			// busca todas as ocorrências da chave na página
			searchFrom := 0
			for {
				idx := strings.Index(pc.lower[searchFrom:], lowerChave)
				if idx == -1 {
					break
				}
				absIdx := searchFrom + idx
				// encontra palavras cobertas pela ocorrência
				// estima startWord e endWord pelo byte index
				startWord := -1
				endWord := -1
				chaveLen := len(lowerChave)
				endIdx := absIdx + chaveLen
				for wi, ws := range pc.wordStarts {
					we := ws + len(pc.words[wi].Texto)
					if ws <= absIdx && absIdx < we {
						startWord = wi
					}
					if ws < endIdx && endIdx <= we {
						endWord = wi
					}
					if ws >= absIdx && we <= endIdx {
						if startWord == -1 {
							startWord = wi
						}
						endWord = wi
					}
				}
				if startWord == -1 {
					// fallback: encontra palavra mais próxima do idx
					for wi, ws := range pc.wordStarts {
						if ws <= absIdx {
							startWord = wi
						}
					}
					if startWord == -1 {
						startWord = 0
					}
					endWord = startWord
				}
				if endWord == -1 {
					endWord = startWord
				}
				if startWord < 0 {
					startWord = 0
				}
				if endWord < startWord {
					endWord = startWord
				}
				if startWord >= len(pc.words) {
					startWord = len(pc.words) - 1
				}
				if endWord >= len(pc.words) {
					endWord = len(pc.words) - 1
				}
				// calcula bbox
				minX0 := pc.words[startWord].X0
				minY0 := pc.words[startWord].Y0
				maxX1 := pc.words[startWord].X1
				maxY1 := pc.words[startWord].Y1
				for wi := startWord + 1; wi <= endWord; wi++ {
					if pc.words[wi].X0 < minX0 {
						minX0 = pc.words[wi].X0
					}
					if pc.words[wi].Y0 < minY0 {
						minY0 = pc.words[wi].Y0
					}
					if pc.words[wi].X1 > maxX1 {
						maxX1 = pc.words[wi].X1
					}
					if pc.words[wi].Y1 > maxY1 {
						maxY1 = pc.words[wi].Y1
					}
				}
				if maxX1 <= minX0 || maxY1 <= minY0 {
					// bbox inválida, ignora
					searchFrom = absIdx + 1
					if searchFrom >= len(pc.lower) {
						break
					}
					continue
				}
				pos := [4]float64{minX0, minY0, maxX1, maxY1}
				occs = append(occs, Ocorrencia{Pagina: pc.num, Pos: &pos, Trecho: base[i].Trecho})
				searchFrom = absIdx + 1
				if searchFrom >= len(pc.lower) {
					break
				}
				if len(occs) >= 10 {
					break
				}
			}
		}
		if len(occs) > 0 {
			base[i].Pagina = occs[0].Pagina
			base[i].Pos = occs[0].Pos
			base[i].Ocorrencias = occs
		}
	}
	return base
}

func extrairSecaoReferencias(texto string) string {
	locs := reRefHeader.FindAllStringIndex(texto, -1)
	if len(locs) == 0 {
		return ""
	}
	first := locs[0]
	start := first[1]
	if start >= len(texto) {
		return ""
	}
	return texto[start:]
}

func snippet(texto string, s, e int) string {
	// Converte índices de bytes para runes para não quebrar UTF-8
	toRuneIdx := func(byteIdx int) int {
		if byteIdx <= 0 {
			return 0
		}
		if byteIdx >= len(texto) {
			return len([]rune(texto))
		}
		return len([]rune(texto[:byteIdx]))
	}
	runes := []rune(texto)
	sr := toRuneIdx(s)
	er := toRuneIdx(e)
	lo := sr - 30
	if lo < 0 {
		lo = 0
	}
	hi := er + 30
	if hi > len(runes) {
		hi = len(runes)
	}
	snip := string(runes[lo:hi])
	snip = strings.ReplaceAll(snip, "\n", " ")
	snip = strings.ReplaceAll(snip, "\r", " ")
	snip = strings.Join(strings.Fields(snip), " ")
	if len([]rune(snip)) > 200 {
		r := []rune(snip)
		snip = string(r[:200])
	}
	return strings.TrimSpace(snip)
}

func containsLetter(s string) bool {
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= 'À' && r <= 'ÿ') {
			return true
		}
	}
	return false
}
