package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/signintech/gopdf"
)

func main() {
	out := filepath.Join("testdata", "artigo-teste.pdf")
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := generate(out); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	fmt.Println("gerado:", out)
}

func fontPath() (string, error) {
	windir := os.Getenv("WINDIR")
	if windir == "" {
		windir = `C:\Windows`
	}
	for _, name := range []string{"arial.ttf", "arialbd.ttf", "calibri.ttf"} {
		p := filepath.Join(windir, "Fonts", name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("nenhuma fonte TTF encontrada em %s\\Fonts", windir)
}

func generate(out string) error {
	font, err := fontPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}

	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	if err := pdf.AddTTFFont("arial", font); err != nil {
		return err
	}

	conteudos := [][]string{
		{
			"Olá mundo! Este é um artigo de teste.",
			"",
			"A plataforma Artigos Ana serve para ler artigos,",
			"marcar trechos importantes e exportar para DOCX.",
			"",
			"Segunda linha com acentuação: ação, coração, atenção.",
		},
		{
			"Página dois do artigo de teste.",
			"",
			"Mais texto para validar a extração página a página.",
			"",
			"Fim do conteúdo.",
		},
	}

	for i, linhas := range conteudos {
		pdf.AddPage()
		y := 90.0
		for _, linha := range linhas {
			pdf.SetX(70)
			pdf.SetY(y)
			pdf.SetFont("arial", "", 13)
			_ = pdf.CellWithOption(&gopdf.Rect{W: 455, H: 20}, linha, gopdf.CellOption{})
			y += 24
		}
		pdf.SetFont("arial", "", 20)
		pdf.SetTextColor(200, 200, 200)
		tr, err := gopdf.NewTransparency(0.18, "")
		if err != nil {
			return err
		}
		pdf.SetTransparency(tr)
		pdf.Rotate(-35, 297, 421)
		pdf.SetX(120)
		pdf.SetY(390)
		_ = pdf.CellWithOption(&gopdf.Rect{W: 360, H: 60}, "MARCA D'ÁGUA", gopdf.CellOption{Align: gopdf.Center})
		pdf.RotateReset()
		pdf.ClearTransparency()
		pdf.SetTextColor(0, 0, 0)
		pdf.SetFont("arial", "", 10)
		pdf.SetX(70)
		pdf.SetY(800)
		_ = pdf.CellWithOption(&gopdf.Rect{W: 455, H: 15}, fmt.Sprintf("Rodapé página %d", i+1), gopdf.CellOption{})
	}

	if err := pdf.Close(); err != nil {
		return err
	}
	return pdf.WritePdf(out)
}
