package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func doReq(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestGovernanca_Health_Bypass_MesmoBloqueado(t *testing.T) {
	origEnforce := os.Getenv("GOVERNANCE_ENFORCE")
	origBind := os.Getenv("BIND_ADDR")
	origIPs := os.Getenv("ALLOWED_IPS")
	origTokens := os.Getenv("ALLOWED_DEVICE_TOKENS")
	defer func() {
		_ = os.Setenv("GOVERNANCE_ENFORCE", origEnforce)
		_ = os.Setenv("BIND_ADDR", origBind)
		_ = os.Setenv("ALLOWED_IPS", origIPs)
		_ = os.Setenv("ALLOWED_DEVICE_TOKENS", origTokens)
	}()
	_ = os.Setenv("GOVERNANCE_ENFORCE", "1")
	_ = os.Setenv("BIND_ADDR", "0.0.0.0")
	_ = os.Setenv("ALLOWED_IPS", "1.2.3.4")
	_ = os.Setenv("ALLOWED_DEVICE_TOKENS", "")

	ts, a := newTestServer(t)
	_, _ = a.DB.DB().Exec(`DELETE FROM dispositivos_autorizados`)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/health", nil)
	req.Header.Set("CF-Connecting-IP", "9.9.9.9")
	resp := doReq(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health sem IP autorizado deveria 200 (enforce), veio %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/health", nil)
	req.Header.Set("CF-Connecting-IP", "9.9.9.9")
	resp2 := doReq(t, req)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("/api/health deveria 200, veio %d", resp2.StatusCode)
	}
}

func TestGovernanca_BloqueioEAllowIP(t *testing.T) {
	origEnforce := os.Getenv("GOVERNANCE_ENFORCE")
	origBind := os.Getenv("BIND_ADDR")
	origIPs := os.Getenv("ALLOWED_IPS")
	origTokens := os.Getenv("ALLOWED_DEVICE_TOKENS")
	defer func() {
		_ = os.Setenv("GOVERNANCE_ENFORCE", origEnforce)
		_ = os.Setenv("BIND_ADDR", origBind)
		_ = os.Setenv("ALLOWED_IPS", origIPs)
		_ = os.Setenv("ALLOWED_DEVICE_TOKENS", origTokens)
	}()
	_ = os.Setenv("GOVERNANCE_ENFORCE", "1")
	_ = os.Setenv("BIND_ADDR", "0.0.0.0")
	_ = os.Setenv("ALLOWED_IPS", "189.6.213.149,192.168.0.94")
	_ = os.Setenv("ALLOWED_DEVICE_TOKENS", "")

	ts, a := newTestServer(t)
	_, _ = a.DB.DB().Exec(`DELETE FROM dispositivos_autorizados`)

	cookie := testLogin(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
	req.Header.Set("CF-Connecting-IP", "8.8.8.8")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp := doReq(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ip nao autorizado deveria 403 veio %d", resp.StatusCode)
	}
	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if !strings.Contains(body["erro"], "dispositivo não autorizado") {
		t.Fatalf("erro esperado acesso restrito veio %v", body)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
	req.Header.Set("CF-Connecting-IP", "192.168.0.94")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp2 := doReq(t, req)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("ip autorizado deveria 200 veio %d", resp2.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
	req.Header.Set("CF-Connecting-IP", "189.6.213.149")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp3 := doReq(t, req)
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("ip publico autorizado deveria 200 veio %d", resp3.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
	req.Header.Set("CF-Connecting-IP", "192.168.0.94")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp4 := doReq(t, req)
	defer resp4.Body.Close()
	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("CF-Connecting-IP autorizado deveria 200 veio %d", resp4.StatusCode)
	}
}

// Uma conta sem senha só pode concluir o primeiro acesso se a governança já
// tiver autorizado a origem. Isto permite o uso no site publicado sem abrir
// cadastro público para IPs ou dispositivos fora da allowlist.
func TestGovernanca_PrimeiraSenhaRemotaExigeOrigemAutorizada(t *testing.T) {
	origEnforce := os.Getenv("GOVERNANCE_ENFORCE")
	origBind := os.Getenv("BIND_ADDR")
	origIPs := os.Getenv("ALLOWED_IPS")
	defer func() {
		_ = os.Setenv("GOVERNANCE_ENFORCE", origEnforce)
		_ = os.Setenv("BIND_ADDR", origBind)
		_ = os.Setenv("ALLOWED_IPS", origIPs)
	}()
	_ = os.Setenv("GOVERNANCE_ENFORCE", "1")
	_ = os.Setenv("BIND_ADDR", "0.0.0.0")
	_ = os.Setenv("ALLOWED_IPS", "203.0.113.7")

	ts, a := newTestServer(t)
	luiz, err := a.DB.GetUsuarioByNome("Luiz")
	if err != nil || luiz == nil {
		t.Fatalf("seed Luiz ausente: %v", err)
	}
	if err := a.DB.SetUsuarioSenha(luiz.ID, ""); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.DB.SetUsuarioSenha(luiz.ID, "") }()

	postLogin := func(ip string) int {
		body := []byte(`{"nome":"Luiz","senha":"senha-inicial-remota"}`)
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("CF-Connecting-IP", ip)
		resp := doReq(t, req)
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if got := postLogin("203.0.113.8"); got != http.StatusForbidden {
		t.Fatalf("origem fora da allowlist deveria 403, veio %d", got)
	}
	if got := postLogin("203.0.113.7"); got != http.StatusOK {
		t.Fatalf("origem autorizada deveria concluir a primeira senha, veio %d", got)
	}
}

func TestGovernanca_XFF_Spoof_Ignorado(t *testing.T) {
	// X-Forwarded-For é controlado pelo cliente: forjar o IP da allowlist
	// NÃO pode liberar. Só CF-Connecting-IP (edge) vale.
	origEnforce := os.Getenv("GOVERNANCE_ENFORCE")
	origBind := os.Getenv("BIND_ADDR")
	origIPs := os.Getenv("ALLOWED_IPS")
	origTokens := os.Getenv("ALLOWED_DEVICE_TOKENS")
	defer func() {
		_ = os.Setenv("GOVERNANCE_ENFORCE", origEnforce)
		_ = os.Setenv("BIND_ADDR", origBind)
		_ = os.Setenv("ALLOWED_IPS", origIPs)
		_ = os.Setenv("ALLOWED_DEVICE_TOKENS", origTokens)
	}()
	_ = os.Setenv("GOVERNANCE_ENFORCE", "1")
	_ = os.Setenv("BIND_ADDR", "0.0.0.0")
	_ = os.Setenv("ALLOWED_IPS", "189.6.213.149")
	_ = os.Setenv("ALLOWED_DEVICE_TOKENS", "")

	ts, a := newTestServer(t)
	_, _ = a.DB.DB().Exec(`DELETE FROM dispositivos_autorizados`)
	cookie := testLogin(t, ts)

	// 1) XFF forjado com IP permitido, sem CF: RemoteAddr é 127.0.0.1 (httptest)
	// mas o caminho de enforcement usa o IP extraído; XFF deve ser ignorado.
	// Como RemoteAddr aqui é loopback, o bypass local permite — então simula
	// origem remota chamando o handler direto com RemoteAddr controlado.
	serve := func(remoteAddr string, setHeaders func(*http.Request)) int {
		req, _ := http.NewRequest(http.MethodGet, "/api/artigos", nil)
		req.RemoteAddr = remoteAddr
		req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
		if setHeaders != nil {
			setHeaders(req)
		}
		rr := httptest.NewRecorder()
		a.Routes().ServeHTTP(rr, req)
		return rr.Code
	}
	if code := serve("9.9.9.9:1234", func(r *http.Request) {
		r.Header.Set("X-Forwarded-For", "189.6.213.149")
	}); code != http.StatusForbidden {
		t.Fatalf("XFF forjado deveria 403 veio %d", code)
	}
	// 2) CF-Connecting-IP permitido libera (edge garante)
	if code := serve("9.9.9.9:1234", func(r *http.Request) {
		r.Header.Set("X-Forwarded-For", "1.1.1.1")
		r.Header.Set("CF-Connecting-IP", "189.6.213.149")
	}); code != http.StatusOK {
		t.Fatalf("CF permitido deveria 200 veio %d", code)
	}
	// 3) CF diferente bloqueia mesmo com XFF permitido
	if code := serve("9.9.9.9:1234", func(r *http.Request) {
		r.Header.Set("X-Forwarded-For", "189.6.213.149")
		r.Header.Set("CF-Connecting-IP", "8.8.8.8")
	}); code != http.StatusForbidden {
		t.Fatalf("CF não autorizado deveria 403 veio %d", code)
	}
	// 4) CF-Connecting-IP vale mesmo em conexão direta: no túnel o RemoteAddr
	// é sempre loopback (só o cloudflared local alcança o backend) e no Render
	// o tráfego chega via Cloudflare — a edge sobrescreve esse cabeçalho.
	if code := serve("9.9.9.9:1234", func(r *http.Request) {
		r.Header.Set("CF-Connecting-IP", "189.6.213.149")
	}); code != http.StatusOK {
		t.Fatalf("CF permitido via direta deveria 200 veio %d", code)
	}
}

func TestGovernanca_Allow_DeviceToken_EnvETabela(t *testing.T) {
	origEnforce := os.Getenv("GOVERNANCE_ENFORCE")
	origBind := os.Getenv("BIND_ADDR")
	origIPs := os.Getenv("ALLOWED_IPS")
	origTokens := os.Getenv("ALLOWED_DEVICE_TOKENS")
	defer func() {
		_ = os.Setenv("GOVERNANCE_ENFORCE", origEnforce)
		_ = os.Setenv("BIND_ADDR", origBind)
		_ = os.Setenv("ALLOWED_IPS", origIPs)
		_ = os.Setenv("ALLOWED_DEVICE_TOKENS", origTokens)
	}()
	_ = os.Setenv("GOVERNANCE_ENFORCE", "1")
	_ = os.Setenv("BIND_ADDR", "0.0.0.0")
	_ = os.Setenv("ALLOWED_IPS", "")
	_ = os.Setenv("ALLOWED_DEVICE_TOKENS", "dev-token-abc-123")

	ts, a := newTestServer(t)
	_, _ = a.DB.DB().Exec(`DELETE FROM dispositivos_autorizados`)

	cookie := testLogin(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
	req.Header.Set("CF-Connecting-IP", "8.8.8.8")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp := doReq(t, req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("sem token deveria 403 veio %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
	req.Header.Set("CF-Connecting-IP", "8.8.8.8")
	req.Header.Set("X-Device-Id", "dev-token-abc-123")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp2 := doReq(t, req)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("token env autorizado deveria 200 veio %d", resp2.StatusCode)
	}

	_ = os.Setenv("ALLOWED_DEVICE_TOKENS", "")
	// via tabela: registra-se o HASH (caminho de produção: registrar hasheia)
	if err := a.DB.EnsureDispositivoAutorizado(a.hashDeviceToken("dev-token-tabela-xyz"), "device_token", "ipad"); err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
	req.Header.Set("CF-Connecting-IP", "8.8.8.8")
	req.Header.Set("X-Device-Id", "dev-token-tabela-xyz")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp3 := doReq(t, req)
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("token tabela autorizado deveria 200 veio %d", resp3.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
	req.Header.Set("CF-Connecting-IP", "8.8.8.8")
	req.Header.Set("X-Device-Token", "dev-token-tabela-xyz")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp4 := doReq(t, req)
	resp4.Body.Close()
	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("X-Device-Token deveria 200 veio %d", resp4.StatusCode)
	}
}

func TestGovernanca_Registrar_Listar(t *testing.T) {
	origEnforce := os.Getenv("GOVERNANCE_ENFORCE")
	origBind := os.Getenv("BIND_ADDR")
	origIPs := os.Getenv("ALLOWED_IPS")
	origTokens := os.Getenv("ALLOWED_DEVICE_TOKENS")
	origJWT := os.Getenv("JWT_SECRET")
	defer func() {
		_ = os.Setenv("GOVERNANCE_ENFORCE", origEnforce)
		_ = os.Setenv("BIND_ADDR", origBind)
		_ = os.Setenv("ALLOWED_IPS", origIPs)
		_ = os.Setenv("ALLOWED_DEVICE_TOKENS", origTokens)
		_ = os.Setenv("JWT_SECRET", origJWT)
	}()
	_ = os.Setenv("GOVERNANCE_ENFORCE", "1")
	_ = os.Setenv("BIND_ADDR", "0.0.0.0")
	_ = os.Setenv("ALLOWED_IPS", "10.0.0.1")
	_ = os.Setenv("JWT_SECRET", "test-jwt-secret-32-bytes-minimo-forte-1234567890")

	ts, a := newTestServer(t)
	_, _ = a.DB.DB().Exec(`DELETE FROM dispositivos_autorizados`)

	cookie := testLogin(t, ts)

	payload := map[string]string{
		"deviceId": "dev-teste-uuid-1234",
		"serial":   "SERIAL-FAKE-001",
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/governanca/registrar", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("CF-Connecting-IP", "10.0.0.1")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp := doReq(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		t.Fatalf("registrar deveria 200 veio %d corpo %s", resp.StatusCode, buf.String())
	}

	ok, _ := a.DB.IsDispositivoAutorizado(a.hashDeviceToken("dev-teste-uuid-1234"), "device_token")
	if !ok {
		t.Fatalf("device token deveria estar autorizado apos registrar")
	}
	// token em claro NUNCA é persistido (vazar o banco não vaza acesso)
	var guardados []string
	rows, err := a.DB.DB().Query(`SELECT identificador FROM dispositivos_autorizados WHERE tipo='device_token'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		guardados = append(guardados, s)
	}
	for _, s := range guardados {
		if s == "dev-teste-uuid-1234" {
			t.Fatalf("token em claro persistido no banco")
		}
	}
	if len(guardados) == 0 {
		t.Fatalf("nenhum device_token gravado")
	}
	hash := a.hashSerial("SERIAL-FAKE-001")
	ok, _ = a.DB.IsDispositivoAutorizado(hash, "serial_hash")
	if !ok {
		t.Fatalf("hash serial deveria estar registrado")
	}

	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/governanca/registrar", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("CF-Connecting-IP", "8.8.8.8")
	resp2 := doReq(t, req)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("registrar sem ip autorizado deveria 403 veio %d", resp2.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/governanca/dispositivos", nil)
	req.Header.Set("CF-Connecting-IP", "10.0.0.1")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp3 := doReq(t, req)
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("listar deveria 200 veio %d", resp3.StatusCode)
	}
	var list []map[string]any
	_ = json.NewDecoder(resp3.Body).Decode(&list)
	if len(list) < 2 {
		t.Fatalf("listar deveria ter >=2 itens (device+hash) veio %d %v", len(list), list)
	}
	for _, item := range list {
		if s, ok := item["identificador"].(string); ok && strings.Contains(s, "SERIAL-FAKE") {
			t.Fatalf("serial em claro vazou na listagem %v", item)
		}
	}
}

func TestGovernanca_DevPermissivo(t *testing.T) {
	origEnforce := os.Getenv("GOVERNANCE_ENFORCE")
	origBind := os.Getenv("BIND_ADDR")
	origIPs := os.Getenv("ALLOWED_IPS")
	defer func() {
		_ = os.Setenv("GOVERNANCE_ENFORCE", origEnforce)
		_ = os.Setenv("BIND_ADDR", origBind)
		_ = os.Setenv("ALLOWED_IPS", origIPs)
	}()
	_ = os.Setenv("GOVERNANCE_ENFORCE", "0")
	_ = os.Setenv("BIND_ADDR", "127.0.0.1")
	_ = os.Setenv("ALLOWED_IPS", "1.2.3.4")

	ts, a := newTestServer(t)
	_, _ = a.DB.DB().Exec(`DELETE FROM dispositivos_autorizados`)

	cookie := testLogin(t, ts)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
	req.Header.Set("CF-Connecting-IP", "9.9.9.9")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp := doReq(t, req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dev permissivo deveria 200 veio %d", resp.StatusCode)
	}
	_ = os.Setenv("GOVERNANCE_ENFORCE", "")
	_ = os.Setenv("BIND_ADDR", "127.0.0.1")
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
	req.Header.Set("CF-Connecting-IP", "9.9.9.9")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp2 := doReq(t, req)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("auto dev deveria 200 veio %d", resp2.StatusCode)
	}
}

func TestGovernanca_CIDR(t *testing.T) {
	origEnforce := os.Getenv("GOVERNANCE_ENFORCE")
	origBind := os.Getenv("BIND_ADDR")
	origIPs := os.Getenv("ALLOWED_IPS")
	defer func() {
		_ = os.Setenv("GOVERNANCE_ENFORCE", origEnforce)
		_ = os.Setenv("BIND_ADDR", origBind)
		_ = os.Setenv("ALLOWED_IPS", origIPs)
	}()
	_ = os.Setenv("GOVERNANCE_ENFORCE", "1")
	_ = os.Setenv("BIND_ADDR", "0.0.0.0")
	_ = os.Setenv("ALLOWED_IPS", "192.168.0.0/24")

	ts, a := newTestServer(t)
	_, _ = a.DB.DB().Exec(`DELETE FROM dispositivos_autorizados`)
	cookie := testLogin(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
	req.Header.Set("CF-Connecting-IP", "192.168.0.55")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp := doReq(t, req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cidr permitido deveria 200 veio %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/artigos", nil)
	req.Header.Set("CF-Connecting-IP", "192.168.1.55")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp2 := doReq(t, req)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("fora cidr deveria 403 veio %d", resp2.StatusCode)
	}
}

func TestGovernanca_HashSerial_NaoVaza(t *testing.T) {
	_ = os.Setenv("JWT_SECRET", "salt-forte-de-teste-32-bytes-1234567890")
	ts, a := newTestServer(t)
	_ = ts
	// usa serials fake para nao vazar serial real mesmo em teste (mascarado)
	h1 := a.hashSerial("PC-FAKE-SERIAL-001")
	h2 := a.hashSerial("IPAD-FAKE-SERIAL-001")
	if h1 == h2 {
		t.Fatalf("hashes de serial diferentes devem diferir")
	}
	if len(h1) != 64 || len(h2) != 64 {
		t.Fatalf("hash SHA256 hex deveria ter 64 chars veio %d %d", len(h1), len(h2))
	}
	if strings.Contains(h1, "PC-FAKE") || strings.Contains(h2, "IPAD-FAKE") {
		t.Fatalf("hash nao deve conter serial em claro")
	}
	if h1 != a.hashSerial("PC-FAKE-SERIAL-001") {
		t.Fatalf("hash deve ser deterministico")
	}
	// verifica que hash salgado diferencia do SHA256 puro do serial
	hRaw := a.hashSerial("PC-FAKE-SERIAL-001")
	_ = os.Setenv("JWT_SECRET", "outro-salt-diferente-1234567890EXTRA")
	hDiff := a.hashSerial("PC-FAKE-SERIAL-001")
	if hRaw == hDiff {
		t.Fatalf("hash deve depender do salt JWT_SECRET")
	}
}
