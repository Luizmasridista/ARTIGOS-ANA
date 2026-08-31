package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const cookieName = "ana_session"

type jwtPayload struct {
	Sub  int64  `json:"sub"`
	Nome string `json:"nome"`
	Exp  int64  `json:"exp"`
	Iat  int64  `json:"iat"`
}

type ctxKey string

const ctxUserIDKey ctxKey = "userID"
const ctxUserNomeKey ctxKey = "userNome"

func isSecureCookie(r *http.Request) bool {
	if os.Getenv("ALLOW_INSECURE_COOKIE") == "1" {
		return false
	}
	if r != nil {
		if r.TLS != nil {
			return true
		}
		if r.Header.Get("X-Forwarded-Proto") == "https" {
			return true
		}
		origin := r.Header.Get("Origin")
		if origin == "null" || origin == "file://" || origin == "" {
			return false
		}
		if strings.HasPrefix(origin, "http://") {
			return false
		}
	}
	return false
}

func issueJWT(secret []byte, userID int64, nome string) (string, error) {
	header := `{"alg":"HS256","typ":"JWT"}`
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(header))
	now := time.Now()
	pl := jwtPayload{Sub: userID, Nome: nome, Exp: now.Add(time.Hour).Unix(), Iat: now.Unix()}
	plJSON, err := json.Marshal(pl)
	if err != nil {
		return "", err
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(plJSON)
	signingInput := headerB64 + "." + payloadB64
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	sig := mac.Sum(nil)
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return signingInput + "." + sigB64, nil
}

func verifyJWT(secret []byte, token string) (int64, string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, "", false
	}
	headerB64, payloadB64, sigB64 := parts[0], parts[1], parts[2]
	// verify alg
	headerJSON, err := base64.RawURLEncoding.DecodeString(headerB64)
	if err != nil {
		return 0, "", false
	}
	var hdr struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerJSON, &hdr); err != nil {
		return 0, "", false
	}
	if hdr.Alg != "HS256" {
		return 0, "", false
	}
	signingInput := headerB64 + "." + payloadB64
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	expectedSig := mac.Sum(nil)
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return 0, "", false
	}
	if !hmac.Equal(sig, expectedSig) {
		return 0, "", false
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return 0, "", false
	}
	var pl jwtPayload
	if err := json.Unmarshal(payloadJSON, &pl); err != nil {
		return 0, "", false
	}
	if time.Now().Unix() > pl.Exp {
		return 0, "", false
	}
	return pl.Sub, pl.Nome, true
}

func (a *App) setAuthCookie(w http.ResponseWriter, r *http.Request, token string) {
	secure := isSecureCookie(r)
	// modo web estrito nunca usa SameSite=None sem Secure
	strict := isStrictCORS()
	origin := ""
	if r != nil {
		origin = r.Header.Get("Origin")
	}
	c := &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   3600,
	}
	if !strict && r != nil && (origin == "null" || origin == "file://") {
		// desktop Electron: precisa None para file://, mas só quando não-estrito
		c.SameSite = http.SameSiteNoneMode
		// SameSite=None exige Secure; em desktop local sem TLS mantém false por compatibilidade, mas loga aviso
		if secure {
			c.Secure = true
		} else {
			c.Secure = false
		}
	}
	http.SetCookie(w, c)
}

func (a *App) clearAuthCookie(w http.ResponseWriter, r *http.Request) {
	secure := isSecureCookie(r)
	strict := isStrictCORS()
	origin := ""
	if r != nil {
		origin = r.Header.Get("Origin")
	}
	c := &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	}
	if !strict && r != nil && (origin == "null" || origin == "file://") {
		c.SameSite = http.SameSiteNoneMode
		if secure {
			c.Secure = true
		} else {
			c.Secure = false
		}
	}
	http.SetCookie(w, c)
}

// rate limiter state
type attemptState struct {
	attempts     int
	firstAttempt time.Time
	lockedUntil  time.Time
}

func (a *App) isBlocked(ip string) (bool, int) {
	a.limiterMu.Lock()
	defer a.limiterMu.Unlock()
	now := time.Now()
	// check memory
	if v, ok := a.limiter.Load(ip); ok {
		st := v.(*attemptState)
		if !st.lockedUntil.IsZero() && now.Before(st.lockedUntil) {
			return true, int(time.Until(st.lockedUntil).Seconds())
		}
		if !st.lockedUntil.IsZero() && now.After(st.lockedUntil) {
			a.limiter.Delete(ip)
			_ = a.DB.DeleteLoginTentativa(ip)
			// continue to check DB below but treat as not blocked
		} else if st.attempts >= 5 && now.Sub(st.firstAttempt) <= 60*time.Second {
			// need to lock on this 6th attempt
			lockUntil := now.Add(15 * time.Minute)
			st.lockedUntil = lockUntil
			a.limiter.Store(ip, st)
			_ = a.DB.UpsertLoginTentativa(ip, st.attempts, st.firstAttempt, &lockUntil)
			return true, 900
		} else if now.Sub(st.firstAttempt) > 60*time.Second {
			// window expired, not blocked, but keep state for recordFailure to reset
			// don't return blocked
		} else {
			// not blocked and not enough attempts
			// still need to check DB maybe has newer lock?
			// but memory is primary
			// fall through to DB check to sync? but if memory has non-blocked state we can return false
			// Check DB for lock that memory doesn't know (e.g., after restart before memory populated)
			// Actually if memory says not blocked, DB might have lock from previous process before restart?
			// But we just loaded memory; after restart memory is empty, so we wouldn't be here.
			// So safe to return false now, but we still want to ensure DB lock is respected if memory is stale.
			// We'll do DB check below before returning false.
		}
		if st != nil && !st.lockedUntil.IsZero() && now.Before(st.lockedUntil) {
			return true, int(time.Until(st.lockedUntil).Seconds())
		}
		// if memory not blocked, check DB for block (in case memory stale after restart)
		// but if we have memory entry with no lock, DB might have lock from earlier before restart? However memory would be empty after restart, so not this branch.
		// So we can continue to DB check only if memory entry indicates not blocked but we want to verify DB.
	}
	lt, err := a.DB.GetLoginTentativa(ip)
	if err != nil || lt == nil {
		return false, 0
	}
	if lt.BloqueadoAte != nil && now.Before(*lt.BloqueadoAte) {
		a.limiter.Store(ip, &attemptState{attempts: lt.Tentativas, firstAttempt: lt.PrimeiraTentativa, lockedUntil: *lt.BloqueadoAte})
		return true, int(time.Until(*lt.BloqueadoAte).Seconds())
	}
	if lt.BloqueadoAte != nil && now.After(*lt.BloqueadoAte) {
		a.limiter.Delete(ip)
		_ = a.DB.DeleteLoginTentativa(ip)
		return false, 0
	}
	if lt.Tentativas >= 5 && now.Sub(lt.PrimeiraTentativa) <= 60*time.Second {
		lockUntil := now.Add(15 * time.Minute)
		st := &attemptState{attempts: lt.Tentativas, firstAttempt: lt.PrimeiraTentativa, lockedUntil: lockUntil}
		a.limiter.Store(ip, st)
		_ = a.DB.UpsertLoginTentativa(ip, lt.Tentativas, lt.PrimeiraTentativa, &lockUntil)
		return true, 900
	}
	if now.Sub(lt.PrimeiraTentativa) > 60*time.Second {
		// window expired, treat as not blocked; cleanup will happen on next failure
		// sync memory
		a.limiter.Store(ip, &attemptState{attempts: lt.Tentativas, firstAttempt: lt.PrimeiraTentativa})
	}
	return false, 0
}

func (a *App) recordFailure(ip string) {
	a.limiterMu.Lock()
	defer a.limiterMu.Unlock()
	now := time.Now()
	var st *attemptState
	if v, ok := a.limiter.Load(ip); ok {
		orig := v.(*attemptState)
		if !orig.lockedUntil.IsZero() && now.Before(orig.lockedUntil) {
			return
		}
		if now.Sub(orig.firstAttempt) > 60*time.Second {
			st = &attemptState{attempts: 1, firstAttempt: now}
		} else {
			st = &attemptState{attempts: orig.attempts + 1, firstAttempt: orig.firstAttempt}
		}
	} else {
		lt, _ := a.DB.GetLoginTentativa(ip)
		if lt != nil {
			if lt.BloqueadoAte != nil && now.Before(*lt.BloqueadoAte) {
				return
			}
			if now.Sub(lt.PrimeiraTentativa) > 60*time.Second || (lt.BloqueadoAte != nil && now.After(*lt.BloqueadoAte)) {
				st = &attemptState{attempts: 1, firstAttempt: now}
			} else {
				st = &attemptState{attempts: lt.Tentativas + 1, firstAttempt: lt.PrimeiraTentativa}
			}
		} else {
			st = &attemptState{attempts: 1, firstAttempt: now}
		}
	}
	a.limiter.Store(ip, st)
	_ = a.DB.UpsertLoginTentativa(ip, st.attempts, st.firstAttempt, nil)
	// if already locked, preserve lock
	if !st.lockedUntil.IsZero() {
		_ = a.DB.UpsertLoginTentativa(ip, st.attempts, st.firstAttempt, &st.lockedUntil)
	}
}

func (a *App) resetAttempts(ip string) {
	a.limiterMu.Lock()
	defer a.limiterMu.Unlock()
	a.limiter.Delete(ip)
	_ = a.DB.DeleteLoginTentativa(ip)
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	log.Printf("[auth] login tentativa ip=%s", ip)
	if blocked, retry := a.isBlocked(ip); blocked {
		log.Printf("[auth] bloqueado ip=%s retry=%d", ip, retry)
		w.Header().Set("Retry-After", "900")
		writeJSON(w, http.StatusLocked, map[string]any{"erro": "muitas tentativas", "retryAfter": retry})
		return
	}
	var in struct {
		Nome string `json:"nome"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeErro(w, http.StatusRequestEntityTooLarge, "corpo muito grande")
			return
		}
		log.Printf("[auth] decode erro ip=%s err=%v", ip, err)
		writeErro(w, http.StatusBadRequest, "selecione um usuário")
		return
	}
	in.Nome = strings.TrimSpace(in.Nome)
	log.Printf("[auth] nome recebido ip=%s nome=%q", ip, in.Nome)
	if in.Nome == "" {
		log.Printf("[auth] nome vazio ip=%s", ip)
		writeErro(w, http.StatusBadRequest, "selecione um usuário")
		return
	}
	if in.Nome != "Ana Bagatinii" && in.Nome != "Luiz" {
		log.Printf("[auth] usuário inválido ip=%s nome=%q", ip, in.Nome)
		a.recordFailure(ip)
		writeErro(w, http.StatusUnauthorized, "usuário inválido")
		return
	}
	u, err := a.DB.GetUsuarioByNome(in.Nome)
	if err != nil {
		log.Printf("[auth] GetUsuarioByNome erro ip=%s nome=%q err=%v", ip, in.Nome, err)
		writeErro(w, http.StatusInternalServerError, "falha ao autenticar")
		return
	}
	if u == nil {
		log.Printf("[auth] usuário não encontrado no DB ip=%s nome=%q", ip, in.Nome)
		a.recordFailure(ip)
		writeErro(w, http.StatusUnauthorized, "usuário inválido")
		return
	}
	log.Printf("[auth] sucesso ip=%s nome=%q id=%d", ip, u.Nome, u.ID)
	a.resetAttempts(ip)
	token, err := issueJWT(a.JWTSecret, u.ID, u.Nome)
	if err != nil {
		log.Printf("[auth] issueJWT erro ip=%s err=%v", ip, err)
		writeErro(w, http.StatusInternalServerError, "falha ao gerar sessão")
		return
	}
	a.setAuthCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": token})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.clearAuthCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func getTokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		return c.Value
	}
	if h := r.Header.Get("Authorization"); h != "" {
		if strings.HasPrefix(strings.ToLower(h), "bearer ") {
			return strings.TrimSpace(h[7:])
		}
		return strings.TrimSpace(h)
	}
	return ""
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	token := getTokenFromRequest(r)
	if token == "" {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	uid, nome, ok := verifyJWT(a.JWTSecret, token)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	u, err := a.DB.GetUsuarioByID(uid)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao buscar usuário")
		return
	}
	if u == nil {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "nome": nome, "authenticated": true})
}

func isPublicPath(path string) bool {
	if path == "/health" || path == "/api/health" {
		return true
	}
	// /api/info só é público em modo desktop (não-estrito); em web estrito exige auth para não vazar IP interno
	if path == "/api/info" {
		if isStrictCORS() {
			return false
		}
		return true
	}
	if strings.HasPrefix(path, "/api/auth/") {
		return true
	}
	return false
}

func (a *App) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			token := getTokenFromRequest(r)
			if token == "" {
				writeErro(w, http.StatusUnauthorized, "não autenticado")
				return
			}
			uid, nome, ok := verifyJWT(a.JWTSecret, token)
			if !ok {
				writeErro(w, http.StatusUnauthorized, "não autenticado")
				return
			}
			ctx := context.WithValue(r.Context(), ctxUserIDKey, uid)
			ctx = context.WithValue(ctx, ctxUserNomeKey, nome)
			r = r.WithContext(ctx)
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
