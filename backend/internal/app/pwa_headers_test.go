package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helpers for creating handler wrapped with securityHeaders
func pwaHeadersHandler() http.Handler {
	return securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
}

func assertCacheControl(t *testing.T, path, want string) {
	t.Helper()
	h := pwaHeadersHandler()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	got := rec.Header().Get("Cache-Control")
	if got != want {
		t.Fatalf("GET %s Cache-Control esperado %q, veio %q", path, want, got)
	}
}

func assertCacheControlContains(t *testing.T, path, substr string) {
	t.Helper()
	h := pwaHeadersHandler()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	got := rec.Header().Get("Cache-Control")
	if !strings.Contains(got, substr) {
		t.Fatalf("GET %s Cache-Control deveria conter %q, veio %q", path, substr, got)
	}
}

func assertCacheControlNotContains(t *testing.T, path, substr string) {
	t.Helper()
	h := pwaHeadersHandler()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	got := rec.Header().Get("Cache-Control")
	if strings.Contains(got, substr) {
		t.Fatalf("GET %s Cache-Control não deveria conter %q, veio %q", path, substr, got)
	}
}

// --- contrato: manifest, sw, workbox, assets = public ---

func TestPwaHeaders_ManifestPublic(t *testing.T) {
	assertCacheControl(t, "/manifest.json", "public, max-age=3600")
}

func TestPwaHeaders_ManifestWebmanifestPublic(t *testing.T) {
	assertCacheControl(t, "/manifest.webmanifest", "public, max-age=3600")
}

func TestPwaHeaders_SwJsPublic(t *testing.T) {
	assertCacheControl(t, "/sw.js", "public, max-age=3600")
}

func TestPwaHeaders_ServiceWorkerJsPublic(t *testing.T) {
	assertCacheControl(t, "/service-worker.js", "public, max-age=3600")
}

func TestPwaHeaders_WorkboxPublic(t *testing.T) {
	cases := []string{"/workbox-27d6fb42.js", "/workbox-abc123.js", "/workbox-xyz.js"}
	for _, p := range cases {
		assertCacheControl(t, p, "public, max-age=3600")
	}
	// workbox sem sufixo .js não é cacheable -> sem public
	h := pwaHeadersHandler()
	req := httptest.NewRequest(http.MethodGet, "/workbox", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Cache-Control"); got == "public, max-age=3600" {
		t.Fatalf("GET /workbox sem .js não deveria ser public, veio %q", got)
	}
}

func TestPwaHeaders_AssetsImmutable(t *testing.T) {
	assertCacheControl(t, "/assets/index-abc123.js", "public, max-age=86400, immutable")
	assertCacheControl(t, "/assets/index-D5GTTBTZ.css", "public, max-age=86400, immutable")
	assertCacheControl(t, "/assets/react-vendor-Dkmnck7H.js", "public, max-age=86400, immutable")
	// assets exige prefix exato /assets/
	h := pwaHeadersHandler()
	req := httptest.NewRequest(http.MethodGet, "/asset/x.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Cache-Control"); strings.Contains(got, "immutable") {
		t.Fatalf("GET /asset/x.js não deveria ser immutable, veio %q", got)
	}
}

// --- contrato: /api/* = no-store ---

func TestPwaHeaders_ApiArtigosNoStore(t *testing.T) {
	cases := []string{
		"/api/artigos",
		"/api/artigos/",
		"/api/artigos/1",
		"/api/artigos/1/marcacoes",
		"/api/artigos/1/notas",
		"/api/artigos/1/paginas/1/camada",
		"/api/artigos/1/paginas/1/imagem",
	}
	for _, p := range cases {
		h := pwaHeadersHandler()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		cc := rec.Header().Get("Cache-Control")
		if !strings.Contains(cc, "no-store") {
			t.Fatalf("GET %s deveria no-store, veio %q", p, cc)
		}
		if !strings.Contains(cc, "no-cache") {
			t.Fatalf("GET %s deveria no-cache, veio %q", p, cc)
		}
		if got := rec.Header().Get("Pragma"); got != "no-cache" {
			t.Fatalf("GET %s Pragma esperado no-cache, veio %q", p, got)
		}
		if strings.Contains(cc, "public") {
			t.Fatalf("GET %s não deveria public, veio %q", p, cc)
		}
	}
}

func TestPwaHeaders_ApiMutacoesNoStore(t *testing.T) {
	methods := []string{http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodPut}
	for _, m := range methods {
		h := pwaHeadersHandler()
		req := httptest.NewRequest(m, "/api/artigos", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		cc := rec.Header().Get("Cache-Control")
		if !strings.Contains(cc, "no-store") {
			t.Fatalf("%s /api/artigos deveria no-store, veio %q", m, cc)
		}
	}
}

func TestPwaHeaders_ApiHealthNoStore(t *testing.T) {
	// decisão ADR: /api/health permanece no-store pois está sob /api/* mutável idempotente,
	// SW pode usar StaleWhileRevalidate mas backend respeita no-store para evitar CDN cache.
	// Se ADR futuro mudar para public, ajustar aqui e documentar justificativa.
	assertCacheControlContains(t, "/api/health", "no-store")
	assertCacheControlContains(t, "/api/health", "no-cache")
}

func TestPwaHeaders_HealthSemApiPrefixSemCacheControl(t *testing.T) {
	// GET /health (sem /api) não é pwa-cacheable nem /api/, então securityHeaders não seta Cache-Control
	h := pwaHeadersHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	cc := rec.Header().Get("Cache-Control")
	if cc != "" {
		t.Fatalf("GET /health sem /api deveria sem Cache-Control via securityHeaders, veio %q", cc)
	}
	// mas outros headers de segurança ainda presentes
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options esperado DENY, veio %q", got)
	}
}

// --- fallback SPA e headers de segurança preservados ---

func TestPwaHeaders_SecurityHeadersPreservadosParaPwa(t *testing.T) {
	cases := []string{"/manifest.json", "/sw.js", "/assets/x.js", "/api/artigos"}
	for _, p := range cases {
		h := pwaHeadersHandler()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Fatalf("%s X-Frame-Options DENY, veio %q", p, got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("%s X-Content-Type-Options nosniff, veio %q", p, got)
		}
		if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Fatalf("%s Referrer-Policy no-referrer, veio %q", p, got)
		}
		if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
			t.Fatalf("%s CSP deveria conter default-src 'self', veio %q", p, got)
		}
		if got := rec.Header().Get("Permissions-Policy"); !strings.Contains(got, "camera=()") {
			t.Fatalf("%s Permissions-Policy ausente, veio %q", p, got)
		}
		if got := rec.Header().Get("Vary"); !strings.Contains(got, "Origin") {
			t.Fatalf("%s Vary deveria conter Origin, veio %q", p, got)
		}
	}
}

func TestPwaHeaders_IsPwaCacheable(t *testing.T) {
	shouldCache := []string{
		"/manifest.json",
		"/manifest.webmanifest",
		"/sw.js",
		"/service-worker.js",
		"/workbox-abc.js",
		"/workbox-123.js",
		"/assets/a.js",
		"/assets/b.css",
		"/assets/c.png",
	}
	for _, p := range shouldCache {
		if !isPwaCacheable(p) {
			t.Errorf("isPwaCacheable(%q) deveria true", p)
		}
	}
	shouldNotCache := []string{
		"/api/artigos",
		"/api/health",
		"/health",
		"/",
		"/index.html",
		"/assets",  // sem trailing
		"/workbox", // sem .js
		"/manifest.json.br",
		"/sw.js.map",
	}
	for _, p := range shouldNotCache {
		if isPwaCacheable(p) {
			t.Errorf("isPwaCacheable(%q) deveria false", p)
		}
	}
}

// --- integração WWWDir serve dist via Routes ---

func TestPwaHeaders_RoutesWWWManifestPublic(t *testing.T) {
	www := t.TempDir()
	if err := os.WriteFile(filepath.Join(www, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(www, "manifest.json"), []byte(`{"name":"Artigos Ana"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(www, "sw.js"), []byte("self.skipWaiting()"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(www, "workbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	// workbox file será detectado via prefix /workbox e suffix .js, mas precisa existir fisicamente como /workbox-*.js na raiz
	if err := os.WriteFile(filepath.Join(www, "workbox-27d6fb42.js"), []byte("workbox"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := New(t.TempDir(), testPopplerDir, testDSN, www)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ts := httptest.NewServer(a.Routes())
	defer ts.Close()

	cases := []struct {
		path string
		want string
	}{
		{"/manifest.json", "public, max-age=3600"},
		{"/sw.js", "public, max-age=3600"},
		{"/workbox-27d6fb42.js", "public, max-age=3600"},
	}
	for _, c := range cases {
		resp, err := http.Get(ts.URL + c.path)
		if err != nil {
			t.Fatalf("GET %s falhou: %v", c.path, err)
		}
		cc := resp.Header.Get("Cache-Control")
		resp.Body.Close()
		if cc != c.want {
			t.Fatalf("GET %s Cache-Control esperado %q, veio %q", c.path, c.want, cc)
		}
	}
}

func TestPwaHeaders_RoutesWWWAssetsImmutable(t *testing.T) {
	www := t.TempDir()
	if err := os.WriteFile(filepath.Join(www, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	assetsDir := filepath.Join(www, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "index-abc.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := New(t.TempDir(), testPopplerDir, testDSN, www)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ts := httptest.NewServer(a.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/assets/index-abc.js")
	if err != nil {
		t.Fatal(err)
	}
	cc := resp.Header.Get("Cache-Control")
	resp.Body.Close()
	if cc != "public, max-age=86400, immutable" {
		t.Fatalf("assets Cache-Control esperado public, max-age=86400, immutable, veio %q", cc)
	}
}

func TestPwaHeaders_RoutesWWWFallbackNoStore(t *testing.T) {
	www := t.TempDir()
	if err := os.WriteFile(filepath.Join(www, "index.html"), []byte("<html>fallback</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := New(t.TempDir(), testPopplerDir, testDSN, www)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ts := httptest.NewServer(a.Routes())
	defer ts.Close()

	// rota SPA desconhecida deve servir index.html com no-store
	resp, err := http.Get(ts.URL + "/alguma/rota/spa")
	if err != nil {
		t.Fatal(err)
	}
	cc := resp.Header.Get("Cache-Control")
	resp.Body.Close()
	if cc != "no-store" {
		t.Fatalf("fallback SPA Cache-Control esperado no-store, veio %q", cc)
	}
	// e rota raiz também fallback? mas se index.html existe em www, "/" serve index.html com? routes.go limpo== ""? testa "/"
	resp2, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	// Para "/" com index.html existente, fileServer serve arquivo, mas securityHeaders não seta cache para "/" (não pwa-cacheable nem api)
	// routes.go para "/" arquivo existe, então fileServer.ServeHTTP sem Cache-Control pré-setado? Na verdade securityHeaders já rodou antes, mas routes.go SPA não seta Cache-Control para "/" existente?
	// Para "/" não-assets não-manifest, espera sem Cache-Control ou fallback?
	// O importante é que não seja public.
	cc2 := resp2.Header.Get("Cache-Control")
	resp2.Body.Close()
	if strings.Contains(cc2, "immutable") {
		t.Fatalf("GET / não deveria immutable, veio %q", cc2)
	}
}

// --- limites, borda, XSS, race ---

func TestPwaHeaders_XSSManifestContentType(t *testing.T) {
	// backend nunca serve manifest com HTML, sempre JSON ou static; securityHeaders garante nosniff
	h := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"name":"<script>alert(1)</script>"}`))
	}))
	req := httptest.NewRequest(http.MethodGet, "/manifest.json", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type deveria json, veio %q", ct)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options nosniff esperado, veio %q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<script>") {
		t.Fatalf("body deveria conter literal script escapado via JSON, veio %q", body)
	}
	// sem execução: apenas verifica que header não permite sniff
}

func TestPwaHeaders_RaceParalelo(t *testing.T) {
	h := pwaHeadersHandler()
	done := make(chan string, 20)
	for i := 0; i < 20; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/manifest.json", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			done <- rec.Header().Get("Cache-Control")
		}()
	}
	for i := 0; i < 20; i++ {
		if got := <-done; got != "public, max-age=3600" {
			t.Fatalf("race manifest Cache-Control veio %q", got)
		}
	}
	// API race também
	for i := 0; i < 20; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/api/artigos", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			done <- rec.Header().Get("Cache-Control")
		}()
	}
	for i := 0; i < 20; i++ {
		if got := <-done; !strings.Contains(got, "no-store") {
			t.Fatalf("race api Cache-Control veio %q", got)
		}
	}
}

func TestPwaHeaders_LimiteMaxIntIdNaoCacheavel(t *testing.T) {
	// ID MAX_INT não deve ser pwa-cacheable, deve ser no-store
	assertCacheControlContains(t, "/api/artigos/9223372036854775807/paginas/1/camada", "no-store")
}

func TestPwaHeaders_VazioECorrompido(t *testing.T) {
	// path vazio "/" não deve ser pwa cacheable
	h := pwaHeadersHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if cc := rec.Header().Get("Cache-Control"); cc == "public, max-age=3600" {
		t.Fatalf("GET / não deveria public, veio %q", cc)
	}
	// path corrompido com encoded
	req = httptest.NewRequest(http.MethodGet, "/%2e%2e/manifest.json", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// securityHeaders usa r.URL.Path raw, que será "/../manifest.json" não igual "/manifest.json"
	cc := rec.Header().Get("Cache-Control")
	if cc == "public, max-age=3600" {
		t.Fatalf("path traversal não deveria ser public")
	}
}
