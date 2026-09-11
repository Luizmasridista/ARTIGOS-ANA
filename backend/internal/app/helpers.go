package app

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErro(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"erro": msg})
}

func parseID(r *http.Request, key string) (int64, bool) {
	v, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

func parseNumero(r *http.Request) (int, bool) {
	v, err := strconv.Atoi(r.PathValue("numero"))
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

var slugReplacer = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
	"Á", "a", "À", "a", "Â", "a", "Ã", "a", "Ä", "a", "Å", "a",
	"É", "e", "È", "e", "Ê", "e", "Ë", "e",
	"Í", "i", "Ì", "i", "Î", "i", "Ï", "i",
	"Ó", "o", "Ò", "o", "Ô", "o", "Õ", "o", "Ö", "o",
	"Ú", "u", "Ù", "u", "Û", "u", "Ü", "u",
	"Ç", "c", "Ñ", "n",
)

func slugify(s string) string {
	s = strings.ToLower(slugReplacer.Replace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if b.Len() > 0 && !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func isStrictCORS() bool {
	if v := strings.TrimSpace(os.Getenv("STRICT_CORS")); v == "1" || strings.EqualFold(v, "true") {
		return true
	}
	if strings.TrimSpace(os.Getenv("ALLOWED_ORIGIN")) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("GROK_ORIGIN")) != "" {
		return true
	}
	return false
}

func allowedOrigins() []string {
	var out []string
	if v := strings.TrimSpace(os.Getenv("ALLOWED_ORIGIN")); v != "" {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
	}
	if v := strings.TrimSpace(os.Getenv("GROK_ORIGIN")); v != "" {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
	}
	out = append(out, "http://127.0.0.1:5173", "http://localhost:5173")
	if !isStrictCORS() {
		// Electron file:// e null origin (file:// carrega via file://) — apenas em modo desktop não-estrito
		out = append(out, "null", "file://")
	}
	return out
}

func isOriginAllowed(origin string, allowed []string) bool {
	for _, a := range allowed {
		if a == origin {
			return true
		}
		if a == "*" {
			return true
		}
	}
	// GROK quick tunnel: permite qualquer subdomínio trycloudflare.com (funciona mesmo em rede diferente)
	if strings.HasSuffix(origin, ".trycloudflare.com") {
		return true
	}
	// Electron file:// e null — sempre permite para desktop, mesmo em estrito (necessário para app desktop com GROK ativo)
	if origin == "null" || origin == "file://" {
		return true
	}
	if isStrictCORS() {
		return false
	}
	// Desktop não-estrito: permite vazio (curl, sem Origin)
	if origin == "" {
		return true
	}
	return false
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := allowedOrigins()
		strict := isStrictCORS()
		if isOriginAllowed(origin, allowed) {
			if origin == "" || origin == "null" {
				if strict {
					// modo web estrito não deve refletir null/vazio
				} else {
					w.Header().Set("Access-Control-Allow-Origin", "null")
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Vary", "Origin")
				}
			} else if origin == "file://" {
				if !strict {
					w.Header().Set("Access-Control-Allow-Origin", "file://")
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Vary", "Origin")
				}
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
			}
		} else if origin == "" && !strict {
			// requisição sem Origin (curl, Electron file:// sem header) — permite apenas em desktop local
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Device-Id, X-Device-Token, X-Device-Serial-Hash, X-Device")
		// só envia Allow-Credentials quando há origin específico autorizado (já setado acima)
		if r.Method == http.MethodOptions {
			// preflight: exige origin permitido em modo estrito
			if strict && !isOriginAllowed(origin, allowed) && origin != "" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isPwaCacheable(path string) bool {
	if path == "/manifest.json" || path == "/manifest.webmanifest" || path == "/sw.js" || path == "/service-worker.js" {
		return true
	}
	if strings.HasPrefix(path, "/workbox") && strings.HasSuffix(path, ".js") {
		return true
	}
	if strings.HasPrefix(path, "/assets/") {
		return true
	}
	return false
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; object-src 'none'; frame-ancestors 'self'; base-uri 'self'; form-action 'self'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("X-DNS-Prefetch-Control", "off")
		// Cache-Control: PWA precache (manifest/sw/workbox/assets) => public max-age=3600 para SW atualizar sem staleness excessivo.
		// API permanece no-store para evitar CDN/proxy cachear dados mutaveis (/api/artigos etc).
		p := r.URL.Path
		if isPwaCacheable(p) {
			if strings.HasPrefix(p, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=3600")
			}
		} else if strings.HasPrefix(p, "/api/") {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
		}
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		}
		// expõe apenas headers necessários para CORS
		w.Header().Set("Vary", "Origin")
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	// Precedencia anti-spoof (túnel e Render passam pela Cloudflare):
	// 1) CF-Connecting-IP — a edge sobrescreve, o cliente não controla;
	// 2) True-Client-IP — proxies que sobrescrevem;
	// 3) RemoteAddr — conexão direta.
	// X-Forwarded-For NUNCA é confiável para decisão de acesso: o cliente
	// controla o início da lista e forjava qualquer IP da allowlist (ver ADR-008).
	if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); v != "" {
		if ip := firstIPField(v); ip != "" {
			return ip
		}
	}
	if v := strings.TrimSpace(r.Header.Get("True-Client-IP")); v != "" {
		if ip := firstIPField(v); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func firstIPField(v string) string {
	if i := strings.Index(v, ","); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// isDirectLoopback diz se a requisição veio do próprio PC sem proxy no caminho
// (Electron em http://127.0.0.1:8734). Usado só em cerimônias sensíveis
// (criar a senha inicial). Com cabeçalho de proxy presente, não é direto.
func isDirectLoopback(r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("CF-Connecting-IP")) != "" ||
		strings.TrimSpace(r.Header.Get("True-Client-IP")) != "" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(strings.TrimSpace(host)); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// --- isolamento multiusuario: validacao central de posse ---

func (a *App) uidFromRequest(r *http.Request) (int64, bool) {
	return getUsuarioID(r)
}

// requireArtigoOwnership verifica se o artigo pertence ao usuario do contexto.
// Se nao pertencer ou nao existir, responde 404 (nao revela existencia) e retorna false.
// Se nao autenticado, responde 401.
func (a *App) requireArtigoOwnership(w http.ResponseWriter, r *http.Request, artigoID int64) bool {
	uid, ok := getUsuarioID(r)
	if !ok || uid == 0 {
		writeErro(w, http.StatusUnauthorized, "não autenticado")
		return false
	}
	owned, err := a.DB.ArtigoOwnedBy(artigoID, uid)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao verificar posse do artigo")
		return false
	}
	if !owned {
		writeErro(w, http.StatusNotFound, "artigo não encontrado")
		return false
	}
	return true
}
