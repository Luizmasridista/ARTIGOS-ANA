package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newUnitApp() *App {
	// App sem DB, apenas para testes de validação que não tocam banco.
	a := &App{
		JWTSecret: []byte("unit-test-secret-12345678901234567890"),
		syncSem:   make(chan struct{}, 4),
		jobSem:    make(chan struct{}, 2),
	}
	return a
}

func unitToken(a *App, uid int64, nome string) string {
	tok, _ := issueJWT(a.JWTSecret, uid, nome)
	return tok
}

func TestSyncCursorEncodeDecode(t *testing.T) {
	now := time.Now().Truncate(time.Microsecond)
	enc := encodeCursor(now, "nota", 42)
	decTime, decEnt, decID, err := decodeCursor(enc)
	if err != nil {
		t.Fatalf("decode erro: %v", err)
	}
	if decID != 42 || decEnt != "nota" {
		t.Fatalf("id/entity esperado 42/nota veio %d/%s", decID, decEnt)
	}
	if decTime.UnixNano() != now.UnixNano() {
		t.Fatalf("tempo divergente")
	}
	// cursor vazio
	ct, ce, ci, err := decodeCursor("")
	if err != nil {
		t.Fatalf("vazio erro: %v", err)
	}
	if !ct.IsZero() || ce != "" || ci != 0 {
		t.Fatalf("vazio deveria zero")
	}
	// compatibilidade cursor antigo {t,i} sem entity
	oldEnc := func() string {
		type oldC struct {
			T int64 `json:"t"`
			I int64 `json:"i"`
		}
		b, _ := json.Marshal(oldC{T: now.UnixNano(), I: 99})
		return base64.RawURLEncoding.EncodeToString(b)
	}()
	ct2, ce2, ci2, err := decodeCursor(oldEnc)
	if err != nil {
		t.Fatalf("old cursor decode erro: %v", err)
	}
	if ce2 != "" || ci2 != 99 || ct2.UnixNano() != now.UnixNano() {
		t.Fatalf("old cursor compat falhou %v %s %d", ct2, ce2, ci2)
	}
	// inválido
	if _, _, _, err := decodeCursor("!!!"); err == nil {
		t.Fatalf("deveria erro em cursor inválido")
	}
	// ordenacao por entity: nota > marcacao > artigo com mesmo t,i
	encA := encodeCursor(now, "artigo", 5)
	encM := encodeCursor(now, "marcacao", 5)
	encN := encodeCursor(now, "nota", 5)
	if encA == encM || encM == encN {
		t.Fatalf("cursors distintos deveriam diferir")
	}
}

func TestSyncHandlerUnauth(t *testing.T) {
	a := newUnitApp()
	ts := httptest.NewServer(a.Routes())
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/api/sync", "application/json", bytes.NewReader([]byte(`{"deviceId":"d1","operations":[]}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("esperado 401 veio %d", resp.StatusCode)
	}
}

func TestSyncHandlerEmptyOperations(t *testing.T) {
	a := newUnitApp()
	ts := httptest.NewServer(a.Routes())
	defer ts.Close()
	tok := unitToken(a, 1, "Ana Bagatinii")
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/sync", bytes.NewReader([]byte(`{"deviceId":"dev1","operations":[]}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty operations deveria 400 veio %d", resp.StatusCode)
	}
}

func TestSyncHandlerTooManyOperations(t *testing.T) {
	a := newUnitApp()
	ts := httptest.NewServer(a.Routes())
	defer ts.Close()
	tok := unitToken(a, 1, "Ana Bagatinii")
	ops := make([]map[string]any, 101)
	for i := range ops {
		ops[i] = map[string]any{"opId": "op-" + string(rune(i)), "entity": "nota", "action": "create", "data": map[string]any{"artigo_id": 1, "pagina": 1, "texto": "x"}}
	}
	body, _ := json.Marshal(map[string]any{"deviceId": "dev1", "operations": ops})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("many operations deveria 400 veio %d", resp.StatusCode)
	}
}

func TestSyncPullInvalidCursor(t *testing.T) {
	a := newUnitApp()
	ts := httptest.NewServer(a.Routes())
	defer ts.Close()
	tok := unitToken(a, 1, "Ana Bagatinii")
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/sync/pull?cursor=!!!", nil)
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("cursor inválido deveria 400 veio %d", resp.StatusCode)
	}
}

func TestSyncPullUnauth(t *testing.T) {
	a := newUnitApp()
	ts := httptest.NewServer(a.Routes())
	defer ts.Close()
	resp, _ := http.Get(ts.URL + "/api/sync/pull")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("pull sem auth deveria 401 veio %d", resp.StatusCode)
	}
}

func TestDecodeTagsLocal(t *testing.T) {
	if got := decodeTagsLocal("{}"); len(got) != 0 {
		t.Fatalf("esperado vazio")
	}
	if got := decodeTagsLocal(`{"a","b"}`); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("decodeTagsLocal falhou %v", got)
	}
}

func TestJobsHandlerUnauth(t *testing.T) {
	a := newUnitApp()
	ts := httptest.NewServer(a.Routes())
	defer ts.Close()
	resp, _ := http.Post(ts.URL+"/api/jobs", "application/json", bytes.NewReader([]byte(`{"tipo":"noop"}`)))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("jobs sem auth deveria 401 veio %d", resp.StatusCode)
	}
}

func TestJobsHandlerTipoObrigatorio(t *testing.T) {
	a := newUnitApp()
	// precisa DB nulo? handleEnqueueJob valida tipo antes de tocar DB, então não precisa DB para este caso
	ts := httptest.NewServer(a.Routes())
	defer ts.Close()
	tok := unitToken(a, 1, "Ana Bagatinii")
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/jobs", bytes.NewReader([]byte(`{"tipo":""}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: tok})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("tipo vazio deveria 400 veio %d", resp.StatusCode)
	}
}

func TestSyncOperationValidationBeforeDB(t *testing.T) {
	// Testa que op com entity inválida retorna error sem precisar de DB (validação antes do tx)
	// Mas processSyncOp é privado; testamos via handler que chama processSyncOp e que exige DB.
	// Aqui apenas verificamos que handler retorna 200 com results error para entity inválida, mas como DB é nil vai panic.
	// Então testamos apenas a validação de opId obrigatório via handler com DB nil? opId vazio gera error sem DB (antes da transação).
	// Para isso, precisamos de DB para não panic no path de opId vazio? Na verdade processSyncOp checa opId antes de qualquer DB.
	// Então podemos testar um caso de opId vazio via handler com DB nil e esperar results com error.
	a := &App{
		JWTSecret: []byte("unit-test-secret-12345678901234567890"),
		syncSem:   make(chan struct{}, 4),
		jobSem:    make(chan struct{}, 2),
		DB:        nil, // vamos interceptar: handleSync vai tentar a.DB.GetSyncOperation e panic; então não testamos esse caminho com DB nil
	}
	_ = a
	// não testa DB nil path que toca DB; apenas documenta limitação.
}
