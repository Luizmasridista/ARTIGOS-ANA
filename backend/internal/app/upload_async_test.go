package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func postArtigoMultipart(t *testing.T, tsURL, cookie string, filename string, conteudo []byte) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(conteudo); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, tsURL+"/api/artigos", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "ana_session", Value: cookie})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestUploadAsync_RetornaJob(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	ts, a := newTestServer(t)
	cookie := testLogin(t, ts)

	// PDF falso mas com magic válido: handler aceita e enfileira (worker falha depois)
	code, out := postArtigoMultipart(t, ts.URL, cookie, "rascunho.pdf", []byte("%PDF-1.4-falso"))
	if code != http.StatusCreated {
		t.Fatalf("upload deveria 201 veio %d %v", code, out)
	}
	if out["status"] != "processando" {
		t.Fatalf("status deveria processando veio %v", out)
	}
	jobID, ok := out["job_id"].(float64)
	if !ok || jobID <= 0 {
		t.Fatalf("job_id ausente veio %v", out)
	}
	if out["num_paginas"] != float64(0) {
		t.Fatalf("num_paginas deveria 0 veio %v", out)
	}
	artID := int64(out["id"].(float64))

	// job existe e pertence à Ana
	ana, _ := a.DB.GetUsuarioByNome("Ana Bagatinii")
	job, found, err := a.DB.GetJob(ana.ID, int64(jobID))
	if err != nil || !found {
		t.Fatalf("job deveria existir: %v", err)
	}
	if job.Tipo != "processar_pdf" {
		t.Fatalf("tipo deveria processar_pdf veio %q", job.Tipo)
	}

	// limpa para o worker de teste não processar depois
	_, _ = a.DB.DeleteArtigo(artID)
	_, _ = a.DB.DB().Exec(`DELETE FROM jobs WHERE id=$1`, int64(jobID))
}

func TestProcessarPDF_Falha_Limpa(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	ts, a := newTestServer(t)
	_ = ts
	ana, _ := a.DB.GetUsuarioByNome("Ana Bagatinii")
	id, _, err := a.DB.CreateArtigoForUser("quebrado", "", ana.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DB.SetArtigoPdfData(id, []byte("%PDF-1.4-lixo")); err != nil {
		t.Fatal(err)
	}
	job, err := a.DB.EnqueueJob(ana.ID, "processar_pdf", json.RawMessage(fmt.Sprintf(`{"artigo_id":%d,"titulo_provisorio":true}`, id)))
	if err != nil {
		t.Fatal(err)
	}
	a.processJob(&job)
	j2, found, err := a.DB.GetJob(ana.ID, job.ID)
	if err != nil || !found {
		t.Fatalf("job deveria existir: %v", err)
	}
	if j2.Status == "done" {
		t.Fatalf("job de pdf inválido não deveria done")
	}
	// artigo removido como no upload síncrono antigo
	var n int
	_ = a.DB.DB().QueryRow(`SELECT COUNT(*) FROM artigos WHERE id=$1`, id).Scan(&n)
	if n != 0 {
		t.Fatalf("artigo com falha deveria ser removido")
	}
}

func TestExtrairTituloDeTexto(t *testing.T) {
	casos := []struct {
		nome string
		in   string
		want string
	}{
		{"titulo simples", "ESTUDO SOBRE O CAFEZINHO DIGITAL\nresumo aqui\n", "ESTUDO SOBRE O CAFEZINHO DIGITAL"},
		{"pula doi e pega titulo", "doi:10.1000/xyz\nJournal of Tests vol. 3\nUM TITULO BEM LONGO E RELEVANTE AQUI\n", "UM TITULO BEM LONGO E RELEVANTE AQUI"},
		{"pula linha curta", "Ana\nESTUDO SOBRE O CAFEZINHO DIGITAL\n", "ESTUDO SOBRE O CAFEZINHO DIGITAL"},
		{"vazio", "\n  \n", ""},
		{"só curto", "Oi\n", ""},
	}
	for _, c := range casos {
		if got := extrairTituloDeTexto(c.in); got != c.want {
			t.Errorf("%s: veio %q queria %q", c.nome, got, c.want)
		}
	}
	// trunca em 200 runes
	longo := strings.Repeat("A", 250) + "\n"
	if got := extrairTituloDeTexto(longo); len([]rune(got)) != 200 {
		t.Errorf("truncamento deveria 200 veio %d", len([]rune(got)))
	}
}

// pdfMinimo gera um PDF válido de 1 página com as linhas dadas (Helvetica base).
func pdfMinimo(linhas []string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := []int{}
	esc := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, "(", `\(`)
		s = strings.ReplaceAll(s, ")", `\)`)
		return s
	}
	var stream bytes.Buffer
	stream.WriteString("BT /F1 24 Tf 72 720 Td ")
	for i, ln := range linhas {
		if i > 0 {
			stream.WriteString("0 -36 Td ")
		}
		stream.WriteString("(" + esc(ln) + ") Tj ")
	}
	stream.WriteString("ET")
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", stream.Len(), stream.String()),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	for _, o := range objs {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", len(offsets), o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objs)+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

func TestProcessarPDF_Sucesso_Titulo(t *testing.T) {
	_ = os.Setenv("ALLOW_INSECURE_COOKIE", "1")
	ts, a := newTestServer(t)
	_ = ts
	if _, err := os.Stat(filepath.Join(a.PopplerDir, "pdftotext.exe")); err != nil {
		if _, err2 := os.Stat(filepath.Join(a.PopplerDir, "pdftotext")); err2 != nil {
			t.Skip("poppler ausente")
		}
	}
	ana, _ := a.DB.GetUsuarioByNome("Ana Bagatinii")
	pdfBytes := pdfMinimo([]string{"ESTUDO SOBRE O CAFEZINHO DIGITAL", "resumo curto aqui"})
	id, _, err := a.DB.CreateArtigoForUser("rascunho", "", ana.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DB.SetArtigoPdfData(id, pdfBytes); err != nil {
		t.Fatal(err)
	}
	job, err := a.DB.EnqueueJob(ana.ID, "processar_pdf", json.RawMessage(fmt.Sprintf(`{"artigo_id":%d,"titulo_provisorio":true}`, id)))
	if err != nil {
		t.Fatal(err)
	}
	a.processJob(&job)
	j2, _, _ := a.DB.GetJob(ana.ID, job.ID)
	if j2.Status != "done" {
		t.Fatalf("job deveria done veio %s erro %v", j2.Status, j2.ErrorText)
	}
	art, found, err := a.DB.GetArtigoByUser(id, ana.ID)
	if err != nil || !found {
		t.Fatalf("artigo deveria existir: %v", err)
	}
	if art.Titulo != "ESTUDO SOBRE O CAFEZINHO DIGITAL" {
		t.Fatalf("título do paper esperado veio %q", art.Titulo)
	}
	var npag int
	_ = a.DB.DB().QueryRow(`SELECT COUNT(*) FROM paginas WHERE artigo_id=$1`, id).Scan(&npag)
	if npag != 1 {
		t.Fatalf("deveria 1 página veio %d", npag)
	}
	_, _ = a.DB.DeleteArtigo(id)
}
