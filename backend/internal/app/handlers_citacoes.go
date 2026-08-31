package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"artigos-ana/backend/internal/citacoes"
	"artigos-ana/backend/internal/store"
)

// duckDuckGoBaseURL pode ser sobrescrito em testes via httptest.Server.
var duckDuckGoBaseURL = "https://html.duckduckgo.com/html/"

var reHref = regexp.MustCompile(`(?i)href\s*=\s*"(https?://[^"]+)"`)
var reUddg = regexp.MustCompile(`(?i)uddg=([^&"]+)`)

// isPrivateIP bloqueia IPs privados, loopback, link-local conforme spec.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// 10.0.0.0/8
		if ip4[0] == 10 {
			return true
		}
		// 172.16.0.0/12
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// 192.168.0.0/16
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		// 127.0.0.0/8
		if ip4[0] == 127 {
			return true
		}
		// 0.0.0.0
		if ip4[0] == 0 && ip4[1] == 0 && ip4[2] == 0 && ip4[3] == 0 {
			return true
		}
		// 169.254.0.0/16 link-local
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	} else {
		// IPv6 checks
		// fe80::/10
		if len(ip) >= 2 && ip[0] == 0xfe && (ip[1]&0xc0) == 0x80 {
			return true
		}
		// fc00::/7 unique local
		if len(ip) >= 1 && (ip[0] == 0xfc || ip[0] == 0xfd) {
			return true
		}
		// ::1 já coberto por IsLoopback, mas garante
		if ip.Equal(net.ParseIP("::1")) {
			return true
		}
	}
	return false
}

func isBlockedHost(host string) bool {
	// host pode ser IP literal ou nome
	if ip := net.ParseIP(host); ip != nil {
		return isPrivateIP(ip)
	}
	// tenta resolver e verifica cada IP
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		// se não resolve, não bloqueia por padrão (pode ser nome válido externo)
		return false
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return true
		}
	}
	return false
}

func buildSearchURL(base, q string) string {
	if strings.Contains(base, "?") {
		// já tem query (raro); acrescenta &q
		if strings.HasSuffix(base, "?") || strings.HasSuffix(base, "&") {
			return base + url.QueryEscape(q)
		}
		return base + "&q=" + url.QueryEscape(q)
	}
	if strings.HasSuffix(base, "/") {
		return base + "?q=" + url.QueryEscape(q)
	}
	return base + "/?q=" + url.QueryEscape(q)
}

func (a *App) textoCompletoDoArtigo(artigoID int64) (string, error) {
	txt, _, err := a.textoEPalavrasPorPagina(artigoID)
	return txt, err
}

func (a *App) textoEPalavrasPorPagina(artigoID int64) (string, []citacoes.PaginaComPalavras, error) {
	paginas, err := a.DB.ListPaginas(artigoID)
	if err != nil {
		return "", nil, err
	}
	var b strings.Builder
	var out []citacoes.PaginaComPalavras
	for _, p := range paginas {
		var words []struct {
			Texto string  `json:"texto"`
			X0    float64 `json:"x0"`
			Y0    float64 `json:"y0"`
			X1    float64 `json:"x1"`
			Y1    float64 `json:"y1"`
		}
		if err := json.Unmarshal(p.CamadaJSON, &words); err == nil && len(words) > 0 {
			for i, w := range words {
				if i > 0 {
					b.WriteString(" ")
				}
				b.WriteString(w.Texto)
			}
			b.WriteString("\n")
			// para pos
			var wps []citacoes.WordPos
			for _, w := range words {
				wps = append(wps, citacoes.WordPos{Texto: w.Texto, X0: w.X0, Y0: w.Y0, X1: w.X1, Y1: w.Y1})
			}
			joined := ""
			for i, w := range words {
				if i > 0 {
					joined += " "
				}
				joined += w.Texto
			}
			out = append(out, citacoes.PaginaComPalavras{Numero: p.Numero, Palavras: wps, Texto: joined})
			continue
		}
		// fallback layout
		layoutPath := a.resolveDataFile(strings.TrimSuffix(p.ImagemPNG, filepath.Ext(p.ImagemPNG)) + ".layout.json")
		if data, err := os.ReadFile(layoutPath); err == nil {
			var linhas []struct {
				Texto string `json:"texto"`
			}
			if err := json.Unmarshal(data, &linhas); err == nil && len(linhas) > 0 {
				for _, l := range linhas {
					if l.Texto != "" {
						b.WriteString(l.Texto)
						b.WriteString("\n")
					}
				}
			}
		}
	}
	return b.String(), out, nil
}

func (a *App) executarVarrerCitacoes(artigoID int64) ([]store.Citacao, error) {
	texto, paginas, err := a.textoEPalavrasPorPagina(artigoID)
	if err != nil {
		return nil, err
	}
	extraidas := citacoes.ExtrairComPosicao(texto, paginas)
	var toSave []store.Citacao
	for _, c := range extraidas {
		var posRaw json.RawMessage
		if c.Pos != nil {
			b, _ := json.Marshal(c.Pos)
			posRaw = json.RawMessage(b)
		}
		var ocorrRaw json.RawMessage
		if len(c.Ocorrencias) > 0 {
			b, _ := json.Marshal(c.Ocorrencias)
			ocorrRaw = json.RawMessage(b)
		}
		toSave = append(toSave, store.Citacao{
			Tipo:        c.Tipo,
			Chave:       c.Chave,
			Autor:       c.Autor,
			Ano:         c.Ano,
			Trecho:      c.Trecho,
			Titulo:      c.Titulo,
			Texto:       c.Texto,
			Pagina:      c.Pagina,
			Pos:         posRaw,
			Ocorrencias: ocorrRaw,
		})
	}
	return a.DB.ReplaceCitacoes(artigoID, toSave)
}

func (a *App) handleListCitacoes(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	list, err := a.DB.ListCitacoes(id)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao listar citações")
		return
	}
	if list == nil {
		list = []store.Citacao{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *App) handleVarrerCitacoes(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	saved, err := a.executarVarrerCitacoes(id)
	if err != nil {
		log.Printf("varrer citacoes falhou artigo %d: %v", id, err)
		writeErro(w, http.StatusInternalServerError, "falha ao varrer citações")
		return
	}
	if saved == nil {
		saved = []store.Citacao{}
	}
	writeJSON(w, http.StatusOK, saved)
}

func (a *App) handleEnriquecerCitacao(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	citID, ok := parseID(r, "citacao_id")
	if !ok {
		writeErro(w, http.StatusBadRequest, "citacao_id inválido")
		return
	}
	if !a.requireArtigoOwnership(w, r, id) {
		return
	}
	cit, found, err := a.DB.GetCitacao(id, citID)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao buscar citação")
		return
	}
	if !found {
		writeErro(w, http.StatusNotFound, "citação não encontrada")
		return
	}
	q := strings.TrimSpace(cit.Titulo)
	if q == "" {
		q = strings.TrimSpace(cit.Texto)
	}
	if q == "" {
		q = strings.TrimSpace(cit.Chave)
	}
	if q == "" {
		q = strings.TrimSpace(cit.Autor)
	}
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]string{"url": ""})
		return
	}

	parsed, err := url.Parse(duckDuckGoBaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		writeJSON(w, http.StatusOK, map[string]string{"url": ""})
		return
	}

	// Cliente com timeout 8s, anti-SSRF nos redirects, limitado a 1 redirect
	client := &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 2 {
				return http.ErrUseLastResponse
			}
			if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
				return fmt.Errorf("esquema de redirect inválido")
			}
			host := req.URL.Hostname()
			if isBlockedHost(host) {
				return fmt.Errorf("redirect bloqueado para IP privado")
			}
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	form := url.Values{}
	form.Set("q", q)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, duckDuckGoBaseURL, strings.NewReader(form.Encode()))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"url": ""})
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		// melhor esforço: retorna vazio
		writeJSON(w, http.StatusOK, map[string]string{"url": ""})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeJSON(w, http.StatusOK, map[string]string{"url": ""})
		return
	}
	// limita a 512KB
	lim := io.LimitReader(resp.Body, 512*1024+1)
	body, err := io.ReadAll(lim)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"url": ""})
		return
	}
	if len(body) > 512*1024 {
		body = body[:512*1024]
	}
	html := string(body)
	matches := reHref.FindAllStringSubmatch(html, -1)
	var foundURL string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		href := m[1]
		if strings.Contains(strings.ToLower(href), "duckduckgo.com") {
			continue
		}
		u, err := url.Parse(href)
		if err != nil {
			continue
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			continue
		}
		if isBlockedHost(u.Hostname()) {
			continue
		}
		foundURL = href
		break
	}
	if foundURL == "" {
		// tenta extrair via uddg (DuckDuckGo redireciona /l/?uddg=https%3A%2F%2F...)
		for _, m := range reUddg.FindAllStringSubmatch(html, -1) {
			if len(m) < 2 {
				continue
			}
			decoded, err := url.QueryUnescape(m[1])
			if err != nil {
				continue
			}
			// uddg pode vir com &amp; escapado
			decoded = strings.Split(decoded, "&")[0]
			decoded = strings.Split(decoded, "\"")[0]
			u, err := url.Parse(decoded)
			if err != nil {
				continue
			}
			if u.Scheme != "http" && u.Scheme != "https" {
				continue
			}
			if strings.Contains(strings.ToLower(decoded), "duckduckgo.com") {
				continue
			}
			if isBlockedHost(u.Hostname()) {
				continue
			}
			foundURL = decoded
			break
		}
	}
	if foundURL != "" {
		_, _ = a.DB.UpdateCitacaoURL(id, citID, foundURL)
		writeJSON(w, http.StatusOK, map[string]string{"url": foundURL})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": ""})
}

func (a *App) handleExcluirLote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeErro(w, http.StatusRequestEntityTooLarge, "corpo muito grande")
			return
		}
		writeErro(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if len(in.IDs) == 0 {
		writeErro(w, http.StatusBadRequest, "ids obrigatório")
		return
	}
	for _, id := range in.IDs {
		if id <= 0 {
			writeErro(w, http.StatusBadRequest, "id inválido")
			return
		}
	}
	// deduplica
	seen := map[int64]bool{}
	var uniq []int64
	for _, id := range in.IDs {
		if !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	for _, id := range uniq {
		uid, ok := getUsuarioID(r)
		if !ok || uid == 0 {
			writeErro(w, http.StatusUnauthorized, "não autenticado")
			return
		}
		owned, err := a.DB.ArtigoOwnedBy(id, uid)
		if err != nil {
			writeErro(w, http.StatusInternalServerError, "falha ao buscar artigo")
			return
		}
		if !owned {
			continue
		}
		if _, err := a.DB.DeleteArtigo(id); err != nil {
			writeErro(w, http.StatusInternalServerError, "falha ao apagar artigo")
			return
		}
		os.Remove(a.pdfPath(id))
		os.RemoveAll(a.paginasPath(id))
		pattern := filepath.Join(a.exportDir, fmt.Sprintf("%d-*.docx", id))
		if matches, _ := filepath.Glob(pattern); matches != nil {
			for _, m := range matches {
				os.Remove(m)
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
