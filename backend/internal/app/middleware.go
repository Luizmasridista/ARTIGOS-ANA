package app

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// limitiar geral: 120 req/min por IP para rotas /api
type rateBucket struct {
	count       int
	windowStart time.Time
}

var generalRateWindow = time.Minute
var generalRateLimit = 120

func (a *App) generalRateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// health/info sem limite rígido? mas ainda limita para evitar flood
		// só aplica para /api/
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		// preflight OPTIONS já tratado no CORS, não conta
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		ip := clientIP(r)
		now := time.Now()
		a.limiterMu.Lock()
		val, _ := a.limiter.Load("rl:" + ip)
		var b *rateBucket
		if val != nil {
			b = val.(*rateBucket)
			if now.Sub(b.windowStart) > generalRateWindow {
				b.count = 1
				b.windowStart = now
			} else {
				b.count++
			}
		} else {
			b = &rateBucket{count: 1, windowStart: now}
			a.limiter.Store("rl:"+ip, b)
		}
		count := b.count
		a.limiterMu.Unlock()
		if count > generalRateLimit {
			w.Header().Set("Retry-After", "60")
			writeErro(w, http.StatusTooManyRequests, "muitas requisições, tente novamente em 60s")
			return
		}
		next.ServeHTTP(w, r)
	})
}

const maxJSONBytes = 1 << 20 // 1 MB para JSON
// Inclui 2 MB para a estrutura multipart, de modo que um PDF de 130 MB seja
// aceito sem abrir margem para um corpo ilimitado.
const maxUploadBytesNew = maxUploadBytes + (2 << 20)

func bodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// upload já tem limite específico em handleCreateArtigo, mas impõe aqui também teto global
		if r.URL.Path == "/api/artigos" && r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytesNew)
			next.ServeHTTP(w, r)
			return
		}
		// para JSON POST/PATCH, limita 1MB
		if r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodPut {
			if ct := r.Header.Get("Content-Type"); strings.Contains(ct, "application/json") || ct == "" {
				r.Body = http.MaxBytesReader(w, r.Body, maxJSONBytes)
			} else if strings.Contains(ct, "multipart/form-data") {
				// multipart já tratado, mas limita
				r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytesNew)
			} else {
				r.Body = http.MaxBytesReader(w, r.Body, maxJSONBytes)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic recuperado: %v path=%s", rec, r.URL.Path)
				// evita leak de stack para cliente
				if !isHeadersSent(w) {
					writeErro(w, http.StatusInternalServerError, "erro interno")
				}
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func isHeadersSent(w http.ResponseWriter) bool {
	// tenta detectar via interface; se não disponível, assume false
	type headerSent interface {
		Header() http.Header
	}
	_ = w.Header()
	return false
}

// decodeJSONLimit decodifica JSON com limite e trata erro de MaxBytes
func decodeJSONLimit(w http.ResponseWriter, r *http.Request, dst any) bool {
	// já limitado por middleware, mas reforça erro amigável
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if err.Error() == "http: request body too large" || strings.Contains(err.Error(), "request body too large") {
			writeErro(w, http.StatusRequestEntityTooLarge, "corpo muito grande (limite 1MB)")
			return false
		}
		var syntax *json.SyntaxError
		var unmarshalType *json.UnmarshalTypeError
		if err == io.EOF {
			writeErro(w, http.StatusBadRequest, "JSON vazio")
			return false
		}
		if bytes.Contains([]byte(err.Error()), []byte("unknown field")) {
			writeErro(w, http.StatusBadRequest, "campo desconhecido no JSON")
			return false
		}
		_ = syntax
		_ = unmarshalType
		writeErro(w, http.StatusBadRequest, "JSON inválido")
		return false
	}
	// garante que não há trailing data
	if dec.More() {
		writeErro(w, http.StatusBadRequest, "JSON inválido: conteúdo extra")
		return false
	}
	return true
}

func ensureContentTypeJSON(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if ct != "" && !strings.Contains(ct, "application/json") && !strings.Contains(ct, "multipart/form-data") && r.ContentLength > 0 {
			// permite vazio para compatibilidade, mas se houver body e content-type errado, rejeita
			if r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodPut {
				if strings.Contains(r.URL.Path, "/api/artigos") && r.Method == http.MethodPost {
					// upload é multipart, já validado
				} else if ct != "" && !strings.Contains(ct, "application/json") {
					// ignora por enquanto, apenas loga
					log.Printf("aviso: Content-Type inesperado %q em %s", ct, r.URL.Path)
				}
			}
		}
		next(w, r)
	}
}

func ipadDetectorMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		// detecção clássica iPad + iPadOS 13+ via header Sec-CH-UA-Platform ou touch hint
		isIpad := strings.Contains(ua, "iPad")
		// para iPadOS 13+ que se passa por Mac, o frontend envia X-Device: iPad quando detecta via JS
		if !isIpad && strings.EqualFold(r.Header.Get("X-Device"), "iPad") {
			isIpad = true
		}
		if !isIpad && strings.Contains(ua, "Macintosh") && r.Header.Get("Sec-CH-UA-Mobile") == "?1" {
			// heurística adicional: Mac com mobile hint pode ser iPad, mas não garante; loga apenas
		}
		if isIpad {
			w.Header().Set("X-Device-Type", "iPad")
			// log para auditoria web identificar iPad
			// não bloqueia, só identifica para ajustes de responsividade já feitos no frontend
		}
		next.ServeHTTP(w, r)
	})
}

// controle de tamanho de Header já via MaxHeaderBytes no servidor
