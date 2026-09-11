package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestCapture_Sniffing_Replay(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	_ = os.Setenv("ALLOWED_ORIGIN", "https://allowed.test")
	defer func() { _ = os.Unsetenv("ALLOWED_ORIGIN") }()
	ts, _ := newTestServer(t)
	// login captura Set-Cookie simulando sniffing passivo em http
	body, _ := json.Marshal(map[string]string{"nome": "Ana Bagatinii", "senha": testSenha})
	req, _ := http.NewRequest("POST", ts.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	sc := resp.Header.Get("Set-Cookie")
	if !strings.Contains(sc, "ana_session=") {
		t.Fatalf("sem cookie %q", sc)
	}
	resp.Body.Close()
	// extrai token
	cookie := ""
	for _, c := range resp.Cookies() {
		if c.Name == "ana_session" {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("cookie vazio")
	}
	// verifica que em http sem TLS, Secure = false (vulnerável a sniffing) - esperado em ALLOW_INSECURE=1
	foundSecureFalse := false
	for _, c := range resp.Cookies() {
		if c.Name == "ana_session" && !c.Secure {
			foundSecureFalse = true
		}
	}
	if !foundSecureFalse {
		t.Logf("Secure true mesmo em http - bom para captura, mas pode quebrar http local")
	}
	// replay de IP diferente - simula atacante que capturou cookie e replaya de outro IP
	req, _ = http.NewRequest("GET", ts.URL+"/api/artigos", nil)
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	req.Header.Set("X-Forwarded-For", "9.9.9.99") // IP diferente do login (login foi 127.0.0.1)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 200 {
		t.Fatalf("replay de IP diferente deveria 200 (sem binding de IP), veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	// replay via Authorization Bearer também funciona (mesma captura)
	req, _ = http.NewRequest("GET", ts.URL+"/api/artigos", nil)
	req.Header.Set("Authorization", "Bearer "+cookie)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 200 {
		t.Fatalf("replay Bearer 200 esperado, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	// demonstra risco: sem TLS, sniffing = hijack total. Com TLS/GROK, sniffing passivo não captura.
	// verifica que com X-Forwarded-Proto https, cookie vem Secure=true
	req2, _ := http.NewRequest("POST", ts.URL+"/api/auth/login", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Forwarded-Proto", "https")
	// precisa desabilitar ALLOW_INSECURE para testar Secure true
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "0")
	resp, _ = http.DefaultClient.Do(req2)
	sc = resp.Header.Get("Set-Cookie")
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	if !strings.Contains(sc, "Secure") {
		t.Errorf("com https, cookie deveria Secure, veio %q", sc)
	}
	resp.Body.Close()
}

func TestCapture_Cache_NoStore(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	_ = os.Unsetenv("ALLOWED_ORIGIN")
	ts, _ := newTestServer(t)
	cookie := testLogin(t, ts)
	req, _ := http.NewRequest("GET", ts.URL+"/api/artigos", nil)
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp, _ := http.DefaultClient.Do(req)
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control deveria no-store para evitar captura em proxy, veio %q", cc)
	}
	if pragma := resp.Header.Get("Pragma"); pragma != "no-cache" {
		t.Errorf("Pragma deveria no-cache, veio %q", pragma)
	}
	resp.Body.Close()
	// imagem também deve ser private (já testado) mas api no-store é prioridade
}

func TestCapture_HSTS_GROK(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	ts, _ := newTestServer(t)
	// sem https, sem HSTS (não força, mas também não protege downgrade)
	req, _ := http.NewRequest("GET", ts.URL+"/api/health", nil)
	resp, _ := http.DefaultClient.Do(req)
	if hsts := resp.Header.Get("Strict-Transport-Security"); hsts != "" {
		t.Errorf("sem https HSTS deveria vazio, veio %q", hsts)
	}
	resp.Body.Close()
	// com GROK https, HSTS presente protege contra downgrade sniffing
	req, _ = http.NewRequest("GET", ts.URL+"/api/health", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, _ = http.DefaultClient.Do(req)
	if hsts := resp.Header.Get("Strict-Transport-Security"); !strings.Contains(hsts, "max-age") {
		t.Errorf("com https HSTS deveria max-age, veio %q", hsts)
	}
	resp.Body.Close()
}

func TestCapture_Session_Fixation(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	ts, _ := newTestServer(t)
	// atacante tenta fixar cookie antes do login
	fixed := "fixed-session-value-evil"
	fixedBody, _ := json.Marshal(map[string]string{"nome": "Ana Bagatinii", "senha": testSenha})
	req, _ := http.NewRequest("POST", ts.URL+"/api/auth/login", bytes.NewReader(fixedBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: fixed})
	resp, _ := http.DefaultClient.Do(req)
	// servidor deve gerar novo token, não reutilizar o fixo
	sc := resp.Header.Get("Set-Cookie")
	if strings.Contains(sc, fixed) {
		t.Errorf("session fixation: servidor reutilizou cookie fixo %q", sc)
	}
	resp.Body.Close()
}

func TestCapture_Authorization_Leak_URL(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	ts, _ := newTestServer(t)
	cookie := testLogin(t, ts)
	// verifica que token não vaza em URL (ex: /api/artigos?token=)
	req, _ := http.NewRequest("GET", ts.URL+"/api/artigos?token="+cookie, nil)
	resp, _ := http.DefaultClient.Do(req)
	// deve 401 porque token na query não é lido (só cookie/header Authorization)
	if resp.StatusCode == 200 {
		// se passar com query, é vazamento via referrer/logs
		t.Errorf("token em URL não deve autenticar, mas deu 200")
	}
	resp.Body.Close()
	// com header Authorization deve funcionar (mas header também pode ser logado em proxy, mas menos que URL)
	req, _ = http.NewRequest("GET", ts.URL+"/api/artigos", nil)
	req.Header.Set("Authorization", "Bearer "+cookie)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 200 {
		t.Errorf("Bearer header deveria 200 veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	// body de login não deve retornar senha (não tem) e token só em Set-Cookie + json mas não em URL
	body, _ := json.Marshal(map[string]string{"nome": "Ana Bagatinii", "senha": testSenha})
	req, _ = http.NewRequest("POST", ts.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	bb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(strings.ToLower(string(bb)), "postgres") || strings.Contains(strings.ToLower(string(bb)), "senha") {
		t.Errorf("login vazou info sensível %s", bb)
	}
}
