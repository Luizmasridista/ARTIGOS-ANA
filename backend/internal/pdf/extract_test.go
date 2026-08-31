package pdf

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/signintech/gopdf"
)

func TestExtractConvertePalavrasParaPixels(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "geo.pdf")
	gerarPDFGeometrico(t, pdfPath)

	popplerDir, err := filepath.Abs(filepath.Join("..", "..", "bin", "poppler"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(popplerDir, "pdftotext.exe")); err != nil {
		t.Skip("poppler não disponível; rode backend/bin/setup-poppler.ps1")
	}

	pagesDir := filepath.Join(dir, "paginas")
	pages, err := Extract(context.Background(), popplerDir, dir, pdfPath, pagesDir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("esperado 1 página, veio %d", len(pages))
	}
	page := pages[0]

	raw, err := palavrasCruas(t, popplerDir, dir, pdfPath)
	if err != nil {
		t.Fatalf("pontos crus: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("nenhuma palavra extraída do PDF de teste")
	}

	// O PNG é renderizado a 150dpi: 1pt = 150/72 px. A camada DEVE estar em
	// pixels do PNG; a regressão clássica era guardar pontos como se fossem px.
	ratioEsperado := 150.0 / 72.0
	if page.WidthPt <= 0 || page.HeightPt <= 0 {
		t.Fatalf("dimensões da página em pontos ausentes: %vx%v", page.WidthPt, page.HeightPt)
	}
	sx := float64(page.WidthPx) / page.WidthPt
	sy := float64(page.HeightPx) / page.HeightPt

	for _, rw := range raw {
		st, ok := acharPorTexto(page.Words, rw.Texto)
		if !ok {
			t.Fatalf("palavra %q presente nos pontos crus mas ausente na camada", rw.Texto)
		}
		ratioX := st.X0 / rw.X0
		if ratioX < ratioEsperado*0.9 || ratioX > ratioEsperado*1.1 {
			t.Errorf("palavra %q: X0 da camada não está em pixels (%.2fpt -> %.2f, ratio %.3f, esperado ~%.3f)",
				rw.Texto, rw.X0, st.X0, ratioX, ratioEsperado)
		}
		if math.Abs(st.X0-rw.X0*sx) > 1.5 {
			t.Errorf("palavra %q: X0 px %.2f fora do esperado %.2f", rw.Texto, st.X0, rw.X0*sx)
		}
		if math.Abs(st.Y0-rw.Y0*sy) > 1.5 {
			t.Errorf("palavra %q: Y0 px %.2f fora do esperado %.2f", rw.Texto, st.Y0, rw.Y0*sy)
		}
		if st.X1 > float64(page.WidthPx) || st.Y1 > float64(page.HeightPx) {
			t.Errorf("palavra %q extrapola a imagem (%d x %d): [%.2f, %.2f]", rw.Texto, page.WidthPx, page.HeightPx, st.X1, st.Y1)
		}
	}
}

func gerarPDFGeometrico(t *testing.T, out string) {
	t.Helper()
	font := fontWindows(t)
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	if err := pdf.AddTTFFont("arial", font); err != nil {
		t.Fatalf("fonte: %v", err)
	}
	pdf.AddPage()
	pdf.SetFont("arial", "", 24)
	for _, p := range []struct {
		x, y float64
		txt  string
	}{
		{100, 100, "ALFA"},
		{300, 400, "BETA"},
	} {
		pdf.SetX(p.x)
		pdf.SetY(p.y)
		if err := pdf.Cell(nil, p.txt); err != nil {
			t.Fatalf("cell: %v", err)
		}
	}
	if err := pdf.WritePdf(out); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func fontWindows(t *testing.T) string {
	t.Helper()
	windir := os.Getenv("WINDIR")
	if windir == "" {
		windir = `C:\Windows`
	}
	for _, name := range []string{"arial.ttf", "arialbd.ttf", "calibri.ttf"} {
		p := filepath.Join(windir, "Fonts", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Fatal("nenhuma fonte TTF encontrada")
	return ""
}

func palavrasCruas(t *testing.T, popplerDir, workDir, pdfPath string) ([]Word, error) {
	t.Helper()
	out, err := os.CreateTemp(workDir, "raw-*.html")
	if err != nil {
		return nil, err
	}
	outPath := out.Name()
	out.Close()
	defer os.Remove(outPath)
	pdftotext := filepath.Join(popplerDir, "pdftotext.exe")
	if err := RunPdfToTextBBox(context.Background(), pdftotext, workDir, pdfPath, outPath); err != nil {
		return nil, err
	}
	f, err := os.Open(outPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	pages, err := ParseBBox(f)
	if err != nil {
		return nil, err
	}
	var words []Word
	for _, p := range pages {
		words = append(words, p.Words...)
	}
	return words, nil
}

func acharPorTexto(words []Word, texto string) (Word, bool) {
	for _, w := range words {
		if w.Texto == texto {
			return w, true
		}
	}
	return Word{}, false
}
