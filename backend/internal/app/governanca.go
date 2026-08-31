package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
)

// isGovernanceEnforced retorna true se middleware deve bloquear acesso nao autorizado.
// Logica: GOVERNANCE_ENFORCE=1|true => forca; =0|false => desliga;
// se nao definido, auto: BIND_ADDR=0.0.0.0 => liga (producao Render), caso contrario desliga (dev).
func isGovernanceEnforced() bool {
	v := strings.TrimSpace(os.Getenv("GOVERNANCE_ENFORCE"))
	if strings.EqualFold(v, "1") || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes") {
		return true
	}
	if strings.EqualFold(v, "0") || strings.EqualFold(v, "false") || strings.EqualFold(v, "no") {
		return false
	}
	if strings.TrimSpace(os.Getenv("BIND_ADDR")) == "0.0.0.0" {
		return true
	}
	return false
}

func parseAllowedList(env string) []string {
	if strings.TrimSpace(env) == "" {
		return nil
	}
	parts := strings.Split(env, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isIPInAllowlist(ip string, list []string) bool {
	if ip == "" || len(list) == 0 {
		return false
	}
	ip = strings.TrimSpace(ip)
	parsed := net.ParseIP(ip)
	for _, entry := range list {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, cidr, err := net.ParseCIDR(entry)
			if err == nil && parsed != nil && cidr.Contains(parsed) {
				return true
			}
			continue
		}
		// compara direto, normaliza: remove porta se houver
		eHost := entry
		if h, _, err := net.SplitHostPort(entry); err == nil {
			eHost = h
		}
		if ip == eHost {
			return true
		}
		// fallback sem ParseIP (ex: 192.168.0.94)
		if ip == entry {
			return true
		}
	}
	return false
}

func maskIP(ip string) string {
	if ip == "" {
		return "***"
	}
	// preserva lo para debug sem vazar: mostra /24 mascarado
	if ip == "127.0.0.1" || ip == "::1" {
		return ip + " (local)"
	}
	if strings.Contains(ip, ".") {
		parts := strings.Split(ip, ".")
		if len(parts) == 4 {
			return parts[0] + "." + parts[1] + ".xxx.xxx"
		}
	}
	if strings.Contains(ip, ":") {
		// ipv6: mostra prefixo
		if len(ip) > 7 {
			return ip[:4] + "xxxx:***"
		}
		return "xxxx:***"
	}
	if len(ip) > 4 {
		return ip[:2] + "***" + ip[len(ip)-2:]
	}
	return "***"
}

func maskDevice(d string) string {
	if d == "" {
		return "-"
	}
	if len(d) <= 8 {
		return "***"
	}
	return d[:2] + "***" + d[len(d)-2:]
}

func isHealthPath(path string) bool {
	return path == "/health" || path == "/api/health"
}

// deviceId extrai token de dispositivo dos headers.
// Aceita X-Device-Id, X-Device-Token, X-Device-ID (case-insensitive via Header.Get).
func extractDeviceId(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Device-Id")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Device-Token")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Device-ID")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Device-Id")); v != "" {
		return v
	}
	// fallback generico lower
	for k, vals := range r.Header {
		lk := strings.ToLower(k)
		if lk == "x-device-id" || lk == "x-device-token" || lk == "x-device-id-token" {
			if len(vals) > 0 && strings.TrimSpace(vals[0]) != "" {
				return strings.TrimSpace(vals[0])
			}
		}
	}
	return ""
}

func (a *App) hashSerial(serial string) string {
	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if secret == "" {
		secret = string(a.JWTSecret)
	}
	s := strings.TrimSpace(serial)
	h := sha256.Sum256([]byte(s + secret))
	return hex.EncodeToString(h[:])
}

// governancaMiddleware bloqueia acesso nao autorizado por IP ou device token.
// Excecoes: /health e /api/health sempre publicos (Render healthcheck),
// OPTIONS preflight sempre liberado, e quando governance nao estiver enforced (dev).
func (a *App) governancaMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// healthcheck sempre publico
		if isHealthPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		// preflight nao deve ser bloqueado (CORS)
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if !isGovernanceEnforced() {
			next.ServeHTTP(w, r)
			return
		}
		ip := clientIP(r)
		// normaliza: remove porta se sobrar
		if h, _, err := net.SplitHostPort(ip); err == nil {
			ip = h
		}
		ip = strings.TrimSpace(ip)
		// loopback sempre liberado para testes/dev e para nao quebrar chamadas internas (ex: httptest 127.0.0.1)
		// Producao Render usa X-Forwarded-For com IP publico, loopback nao afeta seguranca remota.
		if ip != "" {
			if parsed := net.ParseIP(ip); parsed != nil && parsed.IsLoopback() {
				next.ServeHTTP(w, r)
				return
			}
		}
		deviceId := extractDeviceId(r)
		serialHashHeader := strings.TrimSpace(r.Header.Get("X-Device-Serial-Hash"))

		// verifica allowlists via env
		allowedIPs := parseAllowedList(os.Getenv("ALLOWED_IPS"))
		allowedTokens := parseAllowedList(os.Getenv("ALLOWED_DEVICE_TOKENS"))
		// compat: tambem aceita ALLOWED_DEVICE_IDS e DISPOSITIVOS_AUTORIZADOS?
		if len(allowedTokens) == 0 {
			allowedTokens = parseAllowedList(os.Getenv("ALLOWED_DEVICE_IDS"))
		}

		// 1) IP na env allowlist
		if isIPInAllowlist(ip, allowedIPs) {
			next.ServeHTTP(w, r)
			return
		}
		// 2) device token na env allowlist
		if deviceId != "" && isIPInAllowlist(deviceId, allowedTokens) {
			// isIPInAllowlist faz comparacao exata para tokens tambem
			next.ServeHTTP(w, r)
			return
		}
		// 3) verifica tabela se disponivel (best-effort, sem quebrar se DB falhar)
		if a.DB != nil {
			if ip != "" {
				if ok, err := a.DB.IsIPAutorizado(ip); err == nil && ok {
					next.ServeHTTP(w, r)
					return
				}
			}
			if deviceId != "" {
				if ok, err := a.DB.IsDeviceTokenAutorizado(deviceId); err == nil && ok {
					next.ServeHTTP(w, r)
					return
				}
			}
			if serialHashHeader != "" {
				if ok, err := a.DB.IsDispositivoAutorizado(serialHashHeader, "serial_hash"); err == nil && ok {
					next.ServeHTTP(w, r)
					return
				}
				if ok, err := a.DB.IsDispositivoAutorizado(serialHashHeader, "pc_serial_hash"); err == nil && ok {
					next.ServeHTTP(w, r)
					return
				}
				if ok, err := a.DB.IsDispositivoAutorizado(serialHashHeader, "ipad_serial_hash"); err == nil && ok {
					next.ServeHTTP(w, r)
					return
				}
			}
		}

		// bloqueado: log mascarado
		ua := r.Header.Get("User-Agent")
		if len(ua) > 80 {
			ua = ua[:80]
		}
		log.Printf("governanca bloqueado ip=%s ua=%q device=%s path=%s", maskIP(ip), ua, maskDevice(deviceId), r.URL.Path)
		writeErro(w, http.StatusForbidden, "acesso restrito — dispositivo não autorizado")
	})
}

// --- handlers governanca ---

func (a *App) handleGovernancaRegistrar(w http.ResponseWriter, r *http.Request) {
	// autenticado: authMiddleware ja validou, mas garante novamente
	if _, ok := getUsuarioID(r); !ok {
		writeErro(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	var in struct {
		Serial   string `json:"serial"`
		DeviceId string `json:"deviceId"`
		DeviceID string `json:"device_id"`
		IP       string `json:"ip"`
		Tipo     string `json:"tipo"`
	}
	if !decodeJSONLimit(w, r, &in) {
		return
	}
	deviceId := strings.TrimSpace(in.DeviceId)
	if deviceId == "" {
		deviceId = strings.TrimSpace(in.DeviceID)
	}
	// tambem aceita via header se body nao trouxer
	if deviceId == "" {
		deviceId = extractDeviceId(r)
	}
	serial := strings.TrimSpace(in.Serial)
	ipReg := strings.TrimSpace(in.IP)
	tipo := strings.TrimSpace(in.Tipo)

	// pelo menos um identificador deve vir
	if deviceId == "" && serial == "" && ipReg == "" {
		writeErro(w, http.StatusBadRequest, "informe deviceId, serial ou ip")
		return
	}
	// IP explicito: registra como tipo ip
	if ipReg != "" {
		if net.ParseIP(ipReg) == nil && !strings.Contains(ipReg, "/") {
			// tenta validar como IP/CIDR
			if ipReg != strings.TrimSpace(ipReg) {
				ipReg = strings.TrimSpace(ipReg)
			}
		}
		if err := a.DB.EnsureDispositivoAutorizado(ipReg, "ip", "registrado via governanca"); err != nil {
			writeErro(w, http.StatusInternalServerError, "falha ao registrar ip")
			return
		}
	}
	if deviceId != "" {
		if len(deviceId) < 4 || len(deviceId) > 256 {
			writeErro(w, http.StatusBadRequest, "deviceId inválido")
			return
		}
		if err := a.DB.EnsureDispositivoAutorizado(deviceId, "device_token", "registrado via governanca ip="+maskIP(clientIP(r))); err != nil {
			writeErro(w, http.StatusInternalServerError, "falha ao registrar dispositivo")
			return
		}
	}
	if serial != "" {
		if len(serial) < 4 || len(serial) > 64 {
			writeErro(w, http.StatusBadRequest, "serial inválido")
			return
		}
		hash := a.hashSerial(serial)
		// usa tipo generico serial_hash; se tipo solicitado for especifico, respeita
		t := "serial_hash"
		if tipo == "pc_serial_hash" || tipo == "ipad_serial_hash" || tipo == "serial_hash" {
			t = tipo
		}
		if err := a.DB.EnsureDispositivoAutorizado(hash, t, "hash serial registrado device="+maskDevice(deviceId)); err != nil {
			writeErro(w, http.StatusInternalServerError, "falha ao registrar serial")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleGovernancaListar(w http.ResponseWriter, r *http.Request) {
	if _, ok := getUsuarioID(r); !ok {
		writeErro(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	list, err := a.DB.ListDispositivosAutorizados()
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao listar")
		return
	}
	// mascara identificador sensivel na resposta? device_token e ip sao util para admin
	// serial_hash ja e hash, nao vaza serial; mantem como esta
	// para ip, retorna mascarado parcialmente mas tambem com full para admin autenticado? retorna full (auth)
	type out struct {
		ID            int64  `json:"id"`
		Identificador string `json:"identificador"`
		Tipo          string `json:"tipo"`
		Descricao     string `json:"descricao"`
		CriadoEm      string `json:"criado_em"`
	}
	var resp []out
	for _, d := range list {
		// nao expõe hash serial como serial em claro; ja e hash
		resp = append(resp, out{
			ID: d.ID, Identificador: d.Identificador, Tipo: d.Tipo, Descricao: d.Descricao, CriadoEm: d.CriadoEm.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	if resp == nil {
		resp = []out{}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleGovernancaRemover(w http.ResponseWriter, r *http.Request) {
	if _, ok := getUsuarioID(r); !ok {
		writeErro(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	var in struct {
		ID int64 `json:"id"`
	}
	// aceita id via path ou body; tenta path primeiro
	if pid := r.PathValue("id"); pid != "" {
		if v, ok := parseID(r, "id"); ok {
			in.ID = v
		}
	} else {
		_ = json.NewDecoder(r.Body).Decode(&in)
	}
	if in.ID <= 0 {
		writeErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	ok, err := a.DB.DeleteDispositivoAutorizado(in.ID)
	if err != nil {
		writeErro(w, http.StatusInternalServerError, "falha ao remover")
		return
	}
	if !ok {
		writeErro(w, http.StatusNotFound, "não encontrado")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleGovernancaStatus(w http.ResponseWriter, r *http.Request) {
	// endpoint publico? melhor autenticado para nao vazar info
	if _, ok := getUsuarioID(r); !ok {
		writeErro(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	ip := clientIP(r)
	if h, _, err := net.SplitHostPort(ip); err == nil {
		ip = h
	}
	deviceId := extractDeviceId(r)
	allowedIPs := parseAllowedList(os.Getenv("ALLOWED_IPS"))
	allowedTokens := parseAllowedList(os.Getenv("ALLOWED_DEVICE_TOKENS"))
	if len(allowedTokens) == 0 {
		allowedTokens = parseAllowedList(os.Getenv("ALLOWED_DEVICE_IDS"))
	}
	enforce := isGovernanceEnforced()
	via := "bloqueado"
	if !enforce {
		via = "permissivo (GOVERNANCE_ENFORCE=0 ou BIND_ADDR != 0.0.0.0)"
	} else if isIPInAllowlist(ip, allowedIPs) {
		via = "ip env"
	} else if deviceId != "" && isIPInAllowlist(deviceId, allowedTokens) {
		via = "device env"
	} else if a.DB != nil {
		if ok, _ := a.DB.IsIPAutorizado(ip); ok {
			via = "ip tabela"
		} else if deviceId != "" {
			if ok, _ := a.DB.IsDeviceTokenAutorizado(deviceId); ok {
				via = "device tabela"
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enforce": enforce,
		"ip_mascarado": maskIP(ip),
		"device_mascarado": maskDevice(deviceId),
		"via": via,
	})
}
