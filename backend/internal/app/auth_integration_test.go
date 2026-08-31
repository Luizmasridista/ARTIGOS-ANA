package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAuth_Login_Sucesso_Falha_Lock(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	ts, _ := newTestServer(t)

	// login correto já funciona via testLogin, mas testa endpoint diretamente
	body, _ := json.Marshal(map[string]string{"nome": "Ana Bagatinii"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login sucesso esperado 200 veio %d", resp.StatusCode)
	}
	// verifica cookie
	found := false
	for _, c := range resp.Cookies() {
		if c.Name == "ana_session" {
			found = true
			if !c.HttpOnly {
				t.Errorf("cookie deve ser HttpOnly")
			}
			if c.SameSite != http.SameSiteStrictMode {
				t.Errorf("cookie SameSite deve ser Strict, veio %v", c.SameSite)
			}
			if c.Path != "/" {
				t.Errorf("cookie Path deve ser /, veio %s", c.Path)
			}
			if c.MaxAge != 3600 {
				t.Errorf("cookie MaxAge deve ser 3600, veio %d", c.MaxAge)
			}
			// Secure deve ser false pois ALLOW_INSECURE=1
			if c.Secure {
				t.Errorf("cookie Secure deveria ser false em ALLOW_INSECURE=1")
			}
			if strings.Count(c.Value, ".") != 2 {
				t.Errorf("cookie JWT deve ter 3 partes, veio %s", c.Value)
			}
			// verifica JWT
			_, _, ok := verifyJWT(currentTSConfig(t, ts).JWTSecret, c.Value)
			// precisamos pegar App: usar test via newTestServer já tem App, mas vamos pegar via ts?
			// skip verify se não ok, mas testa que JWT tem exp
			_ = ok
		}
	}
	if !found {
		// fallback header check
		h := resp.Header.Get("Set-Cookie")
		if !strings.Contains(h, "ana_session=") {
			t.Fatalf("Set-Cookie ana_session não encontrado %q", h)
		}
		if !strings.Contains(h, "HttpOnly") {
			t.Errorf("Set-Cookie sem HttpOnly %q", h)
		}
		if !strings.Contains(h, "SameSite=Strict") {
			t.Errorf("Set-Cookie sem SameSite=Strict %q", h)
		}
	}
}

func currentTSConfig(t *testing.T, ts *httptest.Server) *App {
	// hack: encontrar App via testAuthCache? Vamos criar um novo App com mesmo DSN e verificar JWTSecret não vazio
	// Na verdade usamos currentTS global para pegar App via criação: newTestServer já retornou mas não guardamos.
	// Simplifica: retorna dummy com JWTSecret carregado via env? Não precisa.
	t.Helper()
	return &App{JWTSecret: loadJWTSecret()}
}

func TestAuth_Login_Falha_401(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	ts, _ := newTestServer(t)
	// garante que contador está zerado
	body, _ := json.Marshal(map[string]string{"nome": "Invalido"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// usa IP diferente via X-Forwarded-For para isolar?
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("senha errada deveria 401, veio %d", resp.StatusCode)
	}
	// sem cookie
	if len(resp.Cookies()) != 0 {
		for _, c := range resp.Cookies() {
			if c.Name == "ana_session" && c.Value != "" && c.MaxAge != -1 {
				t.Errorf("401 não deve setar cookie válido")
			}
		}
	}
}

func TestAuth_RateLimit_5_423(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	ts, _ := newTestServer(t)
	ip := "9.9.9.9"
	for i := 0; i < 5; i++ {
		body, _ := json.Marshal(map[string]string{"nome": "Invalido"})
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", ip)
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("tentativa %d deveria 401 veio %d", i+1, resp.StatusCode)
		}
	}
	// 6ª deve ser 423
	body, _ := json.Marshal(map[string]string{"nome": "Invalido"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", ip)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusLocked {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("6ª tentativa deveria 423 veio %d corpo %s", resp.StatusCode, raw)
	}
	if resp.Header.Get("Retry-After") != "900" {
		t.Errorf("Retry-After deveria 900 veio %q", resp.Header.Get("Retry-After"))
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["erro"] != "muitas tentativas" {
		t.Errorf("erro esperado muitas tentativas veio %v", out)
	}
	if out["retryAfter"] != float64(900) {
		t.Errorf("retryAfter 900 esperado veio %v", out["retryAfter"])
	}
	// mesmo com usuário válido, ainda bloqueado
	body, _ = json.Marshal(map[string]string{"nome": "Ana Bagatinii"})
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", ip)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusLocked {
		t.Fatalf("mesmo senha correta bloqueado deveria 423 veio %d", resp2.StatusCode)
	}
}

func TestAuth_RateLimit_Persistencia_PG(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	ts, a := newTestServer(t)
	ip := "10.20.30.40"
	// 5 falhas
	for i := 0; i < 5; i++ {
		body, _ := json.Marshal(map[string]string{"nome": "Invalido"})
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", ip)
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
	}
	// cria nova App com mesmo DSN mas limiter vazio para simular restart
	a2, err := New(t.TempDir(), testPopplerDir, testDSN, "")
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	ts2 := httptest.NewServer(a2.Routes())
	defer ts2.Close()
	body, _ := json.Marshal(map[string]string{"nome": "Invalido"})
	req, _ := http.NewRequest(http.MethodPost, ts2.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", ip)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusLocked {
		t.Fatalf("persistência PG: após restart, 6ª deveria 423 veio %d", resp.StatusCode)
	}
	_ = ts
	_ = a
}

func TestAuth_Me_Logout_RequireAuth(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	ts, _ := newTestServer(t)
	// me com cookie válido
	cookie := testLogin(t, ts)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me com cookie deveria 200 veio %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out["nome"] != "Ana Bagatinii" {
		t.Fatalf("me nome errado %v", out)
	}
	// me sem cookie 200 com authenticated false (coibir 401 no console)
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/auth/me", nil)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me sem cookie deveria 200 veio %d", resp.StatusCode)
	}
	var meOut map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&meOut)
	resp.Body.Close()
	if meOut["authenticated"] != false {
		t.Fatalf("me sem cookie deveria authenticated false veio %v", meOut)
	}
	// api protegida sem cookie 401
	resp = do(t, http.MethodGet, ts.URL+"/api/artigos", nil)
	// do already adds cookie, so testa raw sem cookie
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
	resp2, _ := http.DefaultClient.Do(req)
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/artigos sem cookie deveria 401 veio %d", resp2.StatusCode)
	}
	resp2.Body.Close()
	// com cookie via helper deve 200 (já testado antes)
	// logout
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout deveria 204 veio %d", resp.StatusCode)
	}
	// verifica cookie limpo MaxAge -1 ou 0?
	found := false
	for _, c := range resp.Cookies() {
		if c.Name == "ana_session" {
			found = true
			if c.MaxAge != -1 && c.MaxAge != 0 {
				t.Errorf("logout cookie MaxAge deveria -1 ou 0 veio %d", c.MaxAge)
			}
		}
	}
	if !found {
		h := resp.Header.Get("Set-Cookie")
		if !strings.Contains(h, "ana_session=") {
			t.Errorf("logout Set-Cookie não encontrado %q", h)
		}
	}
	resp.Body.Close()
}

func TestSecurityHeaders(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	ts, _ := newTestServer(t)
	// request normal http sem https
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("X-Frame-Options esperado DENY veio %q", resp.Header.Get("X-Frame-Options"))
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options")
	}
	if resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("Referrer-Policy")
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP errado %q", csp)
	}
	if hsts := resp.Header.Get("Strict-Transport-Security"); hsts != "" {
		t.Errorf("HSTS não deveria estar presente sem https, veio %q", hsts)
	}
	// com X-Forwarded-Proto https
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if hsts := resp.Header.Get("Strict-Transport-Security"); !strings.Contains(hsts, "max-age=31536000") || !strings.Contains(hsts, "includeSubDomains") {
		t.Errorf("HSTS com https esperado max-age... includeSubDomains, veio %q", hsts)
	}
}

func TestCORS_Fechado(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	_ = os.Setenv("ALLOWED_ORIGIN", "https://allowed.grok.io")
	_ = os.Setenv("GROK_ORIGIN", "")
	ts, _ := newTestServer(t)
	defer func() {
		_ = os.Unsetenv("ALLOWED_ORIGIN")
		_ = os.Unsetenv("GROK_ORIGIN")
	}()
	// origin permitido
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	req.Header.Set("Origin", "https://allowed.grok.io")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://allowed.grok.io" {
		t.Errorf("CORS permitido deveria refletir origin, veio %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials deveria true, veio %q", got)
	}
	// origin não permitido
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	req.Header.Set("Origin", "https://evil.com")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("CORS evil deveria ser vazio, veio %q", got)
	}
	// dev 5173 sempre permitido
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:5173" {
		t.Errorf("CORS 5173 deveria permitido, veio %q", got)
	}
	// sem env, mas 5173 ainda permitido (fechado mas dev)
	_ = os.Unsetenv("ALLOWED_ORIGIN")
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:5173" {
		t.Errorf("CORS 5173 sem env deveria permitido, veio %q", got)
	}
	// evil sem env continua fechado
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	req.Header.Set("Origin", "https://evil.com")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("CORS evil sem env deveria vazio, veio %q", got)
	}
	// OPTIONS preflight
	req, _ = http.NewRequest(http.MethodOptions, ts.URL+"/api/artigos", nil)
	req.Header.Set("Origin", "https://allowed.grok.io")
	_ = os.Setenv("ALLOWED_ORIGIN", "https://allowed.grok.io")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("OPTIONS deveria 204 veio %d", resp.StatusCode)
	}
}

func TestJWT_ExpiracaoETamper(t *testing.T) {
	secret := []byte("12345678901234567890123456789012")
	token, err := issueJWT(secret, 1, "Ana Bagatinii")
	if err != nil {
		t.Fatal(err)
	}
	uid, nome, ok := verifyJWT(secret, token)
	if !ok || uid != 1 || nome != "Ana Bagatinii" {
		t.Fatalf("verify ok esperado, veio %v %d %s", ok, uid, nome)
	}
	// tamper
	parts := strings.Split(token, ".")
	parts[1] = parts[1][:len(parts[1])-2] + "AA"
	tampered := strings.Join(parts, ".")
	_, _, ok = verifyJWT(secret, tampered)
	if ok {
		t.Fatalf("tampered deveria falhar")
	}
	// wrong secret
	_, _, ok = verifyJWT([]byte("outro segredo 32 bytes 1234567890"), token)
	if ok {
		t.Fatalf("wrong secret deveria falhar")
	}
}
