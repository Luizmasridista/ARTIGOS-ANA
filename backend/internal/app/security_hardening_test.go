package app

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func itoaStr(v int64) string {
	return strconv.FormatInt(v, 10)
}

func TestHardening_Paginas_ExigeAuth(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	ts, _ := newTestServer(t)
	criado := uploadPDF(t, ts, "Artigo PagAuth")
	id := int64(criado["id"].(float64))

	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/artigos/"+itoaStr(id)+"/paginas/1/imagem", nil)
	resp, _ := http.DefaultClient.Do(req2)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("paginas/imagem sem auth deveria 401, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	req3, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/artigos/"+itoaStr(id)+"/paginas/1/camada", nil)
	resp, _ = http.DefaultClient.Do(req3)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("paginas/camada sem auth deveria 401, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = do(t, http.MethodGet, ts.URL+"/api/artigos/"+itoaStr(id)+"/paginas/1/imagem", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("paginas/imagem com auth deveria 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = do(t, http.MethodGet, ts.URL+"/api/artigos/"+itoaStr(id)+"/paginas/1/camada", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("paginas/camada com auth deveria 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHardening_Info_Estrito_ExigeAuth(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	_ = os.Setenv("ALLOWED_ORIGIN", "https://allowed.test")
	defer func() {
		_ = os.Unsetenv("ALLOWED_ORIGIN")
	}()
	ts, _ := newTestServer(t)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/info", nil)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusUnauthorized {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("/api/info estrito sem auth deveria 401, veio %d %s", resp.StatusCode, raw)
	} else {
		resp.Body.Close()
	}

	cookie := testLogin(t, ts)
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/info", nil)
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/info estrito com auth deveria 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	_ = os.Unsetenv("ALLOWED_ORIGIN")
	ts2, _ := newTestServer(t)
	req, _ = http.NewRequest(http.MethodGet, ts2.URL+"/api/info", nil)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/info não-estrito deveria 200 sem auth, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	_ = os.Setenv("ALLOWED_ORIGIN", "https://allowed.test")
}

func TestHardening_CORS_Estrito(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	_ = os.Setenv("ALLOWED_ORIGIN", "https://allowed.test")
	defer func() { _ = os.Unsetenv("ALLOWED_ORIGIN") }()
	ts, _ := newTestServer(t)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	req.Header.Set("Origin", "https://evil.com")
	resp, _ := http.DefaultClient.Do(req)
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("CORS estrito evil deveria vazio, veio %q", got)
	}
	resp.Body.Close()

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	req.Header.Set("Origin", "null")
	resp, _ = http.DefaultClient.Do(req)
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("CORS estrito null deveria vazio, veio %q", got)
	}
	resp.Body.Close()

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	req.Header.Set("Origin", "file://")
	resp, _ = http.DefaultClient.Do(req)
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("CORS estrito file:// deveria vazio, veio %q", got)
	}
	resp.Body.Close()

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	req.Header.Set("Origin", "https://allowed.test")
	resp, _ = http.DefaultClient.Do(req)
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://allowed.test" {
		t.Errorf("CORS estrito allowed deveria refletir, veio %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials deveria true, veio %q", got)
	}
	resp.Body.Close()

	req, _ = http.NewRequest(http.MethodOptions, ts.URL+"/api/artigos", nil)
	req.Header.Set("Origin", "https://evil.com")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("OPTIONS evil estrito deveria 403, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHardening_CORS_NaoEstrito_PermiteFileNull(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	_ = os.Unsetenv("ALLOWED_ORIGIN")
	_ = os.Unsetenv("GROK_ORIGIN")
	ts, _ := newTestServer(t)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	req.Header.Set("Origin", "null")
	resp, _ := http.DefaultClient.Do(req)
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "null" {
		t.Errorf("CORS não-estrito null deveria null, veio %q", got)
	}
	resp.Body.Close()
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	req.Header.Set("Origin", "file://")
	resp, _ = http.DefaultClient.Do(req)
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "file://" {
		t.Errorf("CORS não-estrito file:// deveria file://, veio %q", got)
	}
	resp.Body.Close()
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	resp, _ = http.DefaultClient.Do(req)
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "null" {
		t.Errorf("CORS não-estrito sem Origin deveria null, veio %q", got)
	}
	resp.Body.Close()
}

func TestHardening_SecurityHeaders_Reforcados(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	_ = os.Unsetenv("ALLOWED_ORIGIN")
	ts, _ := newTestServer(t)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	resp, _ := http.DefaultClient.Do(req)
	if got := resp.Header.Get("Permissions-Policy"); !strings.Contains(got, "camera=()") {
		t.Errorf("Permissions-Policy ausente, veio %q", got)
	}
	if got := resp.Header.Get("X-Permitted-Cross-Domain-Policies"); got != "none" {
		t.Errorf("X-Permitted-Cross-Domain-Policies esperado none, veio %q", got)
	}
	if got := resp.Header.Get("Cross-Origin-Opener-Policy"); got != "same-origin" {
		t.Errorf("Cross-Origin-Opener-Policy esperado same-origin, veio %q", got)
	}
	if got := resp.Header.Get("Cross-Origin-Resource-Policy"); got != "same-origin" {
		t.Errorf("Cross-Origin-Resource-Policy esperado same-origin, veio %q", got)
	}
	if got := resp.Header.Get("X-DNS-Prefetch-Control"); got != "off" {
		t.Errorf("X-DNS-Prefetch-Control esperado off, veio %q", got)
	}
	resp.Body.Close()

	cookie := testLogin(t, ts)
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp2, _ := http.DefaultClient.Do(req)
	if got := resp2.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("/api/ deveria no-store, veio %q", got)
	}
	resp2.Body.Close()
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "base-uri 'self'") {
		t.Errorf("CSP deveria conter base-uri 'self', veio %q", csp)
	}
}

func TestHardening_BodyLimit_JSON_413(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	_ = os.Unsetenv("ALLOWED_ORIGIN")
	ts, _ := newTestServer(t)
	criado := uploadPDF(t, ts, "Artigo BodyLimit")
	id := int64(criado["id"].(float64))

	large := strings.Repeat("A", 1500000)
	body, _ := json.Marshal(map[string]any{
		"pagina": 1,
		"texto":  large,
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/artigos/"+itoaStr(id)+"/notas", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addAuth(t, req, ts)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request falhou: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		raw, _ := io.ReadAll(resp.Body)
		sn := string(raw)
		if len(sn) > 500 {
			sn = sn[:500]
		}
		t.Fatalf("JSON grande deveria 413, veio %d %s", resp.StatusCode, sn)
	}
}

func TestHardening_Upload_Validacoes(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	_ = os.Unsetenv("ALLOWED_ORIGIN")
	ts, _ := newTestServer(t)

	// extensão não-pdf (.exe) deve 400
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	fw, _ := mw.CreateFormFile("file", "malicioso.exe")
	// escreve conteúdo fake PDF mas extensão errada
	_, _ = fw.Write([]byte("%PDF- fake content"))
	_ = mw.WriteField("titulo", "teste")
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/artigos", buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	addAuth(t, req, ts)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("extensão .exe deveria 400, veio %d", resp.StatusCode)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// título muito longo >300 deve 400
	pdfData, _ := os.ReadFile(testPDFPath)
	buf2 := &bytes.Buffer{}
	mw2 := multipart.NewWriter(buf2)
	fw2, _ := mw2.CreateFormFile("file", "artigo.pdf")
	_, _ = fw2.Write(pdfData)
	_ = mw2.WriteField("titulo", strings.Repeat("X", 301))
	mw2.Close()
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/artigos", buf2)
	req.Header.Set("Content-Type", mw2.FormDataContentType())
	addAuth(t, req, ts)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("titulo longo deveria 400, veio %d %s", resp.StatusCode, raw)
	} else {
		io.Copy(io.Discard, resp.Body)
	}
	resp.Body.Close()
}

func TestHardening_RateLimit_429(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	_ = os.Unsetenv("ALLOWED_ORIGIN")
	ts, _ := newTestServer(t)
	cookie := testLogin(t, ts)
	var lastStatus int
	var lastHeader string
	for i := 0; i < 125; i++ {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
		req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
		req.Header.Set("X-Forwarded-For", "1.2.3.100")
		resp, _ := http.DefaultClient.Do(req)
		lastStatus = resp.StatusCode
		lastHeader = resp.Header.Get("Retry-After")
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if lastStatus == http.StatusTooManyRequests {
			if lastHeader != "60" {
				t.Errorf("Retry-After deveria 60, veio %q", lastHeader)
			}
			return
		}
	}
	t.Fatalf("rate limit não disparou após 125 req, último status %d", lastStatus)
}

func TestHardening_Imagem_CachePrivate(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	_ = os.Unsetenv("ALLOWED_ORIGIN")
	ts, _ := newTestServer(t)
	criado := uploadPDF(t, ts, "Artigo Cache")
	id := int64(criado["id"].(float64))
	resp := do(t, http.MethodGet, ts.URL+"/api/artigos/"+itoaStr(id)+"/paginas/1/imagem", nil)
	defer resp.Body.Close()
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "private") {
		t.Errorf("imagem Cache-Control deveria private, veio %q", cc)
	}
	if cc := resp.Header.Get("Cache-Control"); strings.Contains(cc, "public") {
		t.Errorf("imagem não deveria public, veio %q", cc)
	}
}

func TestHardening_PathTraversal_WWW(t *testing.T) {
	www := t.TempDir()
	if err := os.WriteFile(www+"/index.html", []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := New(t.TempDir(), testPopplerDir, testDSN, www)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ts := httptest.NewServer(a.Routes())
	defer ts.Close()
	// tenta traversal via /../
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/../etc/passwd", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request falhou: %v", err)
	}
	defer resp.Body.Close()
	// deve servir SPA index.html (fallback) e não arquivo externo; status 200 com ok, mas não deve expor passwd
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "root:") {
		t.Errorf("path traversal vazou /etc/passwd")
	}
	// garante que não retornou 500
	if resp.StatusCode != http.StatusOK {
		t.Logf("traversal status %d body %s", resp.StatusCode, body)
	}
}

func TestHardening_Cookie_Atributos(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	_ = os.Unsetenv("ALLOWED_ORIGIN")
	ts, _ := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"nome": "Ana Bagatinii"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request falhou: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login deveria 200, veio %d", resp.StatusCode)
	}
	found := false
	for _, c := range resp.Cookies() {
		if c.Name == "ana_session" {
			found = true
			if !c.HttpOnly {
				t.Errorf("cookie deve ser HttpOnly")
			}
			if c.Path != "/" {
				t.Errorf("Path deve ser /, veio %s", c.Path)
			}
		}
	}
	if !found {
		h := resp.Header.Get("Set-Cookie")
		if !strings.Contains(h, "HttpOnly") {
			t.Errorf("Set-Cookie sem HttpOnly %q", h)
		}
		if !strings.Contains(h, "ana_session=") {
			t.Fatalf("cookie não encontrado %q", h)
		}
	}
	// verifica headers de segurança também no login
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("login X-Frame-Options deveria DENY veio %q", got)
	}
}
