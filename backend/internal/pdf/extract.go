package pdf

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const renderDPI = "150"

func renderDPIFloat() float64 {
	dpi, _ := strconv.ParseFloat(renderDPI, 64)
	return dpi / 72
}

type ExtractedPage struct {
	Numero     int
	PNGPath    string
	CamadaPath string
	Words      []Word
	WidthPx    int
	HeightPx   int
	WidthPt    float64
	HeightPt   float64
}

func RunPdfToTextBBox(ctx context.Context, pdftotext, workDir, pdfPath, outPath string) error {
	relPDF, err := filepath.Rel(workDir, pdfPath)
	if err != nil {
		return err
	}
	relOut, err := filepath.Rel(workDir, outPath)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, pdftotext, "-bbox", "-enc", "UTF-8", relPDF, relOut)
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pdftotext falhou: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func RunPdfToPPM(ctx context.Context, pdftoppm, workDir, pdfPath string, page int, outPrefix string) error {
	relPDF, err := filepath.Rel(workDir, pdfPath)
	if err != nil {
		return err
	}
	relPrefix, err := filepath.Rel(workDir, outPrefix)
	if err != nil {
		return err
	}
	args := []string{"-png", "-r", renderDPI, "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), relPDF, relPrefix}
	cmd := exec.CommandContext(ctx, pdftoppm, args...)
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pdftoppm falhou: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func RunPdfToTextBBoxLayout(ctx context.Context, pdftotext, workDir, pdfPath, outPath string) error {
	relPDF, err := filepath.Rel(workDir, pdfPath)
	if err != nil {
		return err
	}
	relOut, err := filepath.Rel(workDir, outPath)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, pdftotext, "-bbox-layout", "-enc", "UTF-8", relPDF, relOut)
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pdftotext layout falhou: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func resolvePopplerBin(dir, name string) string {
	candidates := []string{}
	if dir != "" {
		candidates = append(candidates, filepath.Join(dir, name+".exe"))
		candidates = append(candidates, filepath.Join(dir, name))
	}
	candidates = append(candidates, name)
	candidates = append(candidates, name+".exe")
	for _, c := range candidates {
		if strings.Contains(c, string(os.PathSeparator)) {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		} else {
			if p, err := exec.LookPath(c); err == nil {
				return p
			}
		}
	}
	if dir != "" {
		return filepath.Join(dir, name)
	}
	return name
}

func hasPopplerBin(bin string) bool {
	if strings.Contains(bin, string(os.PathSeparator)) {
		_, err := os.Stat(bin)
		return err == nil
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

func Extract(ctx context.Context, popplerDir, workDir, pdfPath, pagesDir string) ([]ExtractedPage, error) {
	pdftotext := resolvePopplerBin(popplerDir, "pdftotext")
	pdftoppm := resolvePopplerBin(popplerDir, "pdftoppm")
	if !hasPopplerBin(pdftotext) {
		return nil, fmt.Errorf("poppler não encontrado (%s em %s); rode bin/setup-poppler.ps1 ou instale poppler-utils (apt: poppler-utils)", pdftotext, popplerDir)
	}
	if !hasPopplerBin(pdftoppm) {
		return nil, fmt.Errorf("poppler não encontrado (%s em %s); rode bin/setup-poppler.ps1 ou instale poppler-utils (apt: poppler-utils)", pdftoppm, popplerDir)
	}
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		return nil, err
	}

	bboxFile, err := os.CreateTemp(pagesDir, "bbox-*.html")
	if err != nil {
		return nil, err
	}
	bboxPath := bboxFile.Name()
	bboxFile.Close()
	defer os.Remove(bboxPath)

	if err := RunPdfToTextBBox(ctx, pdftotext, workDir, pdfPath, bboxPath); err != nil {
		return nil, err
	}
	f, err := os.Open(bboxPath)
	if err != nil {
		return nil, err
	}
	pages, err := ParseBBox(f)
	f.Close()
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("nenhuma página extraída do PDF")
	}

	layoutFile, err := os.CreateTemp(pagesDir, "layout-*.html")
	if err != nil {
		return nil, err
	}
	layoutPath := layoutFile.Name()
	layoutFile.Close()
	defer os.Remove(layoutPath)

	layoutByPage := map[int][]LayoutLine{}
	if err := RunPdfToTextBBoxLayout(ctx, pdftotext, workDir, pdfPath, layoutPath); err == nil {
		lf, err := os.Open(layoutPath)
		if err == nil {
			layoutByPage, err = ParseBBoxLayout(lf)
			lf.Close()
		}
	}

	renderDir, err := os.MkdirTemp(pagesDir, "render-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(renderDir)

	var result []ExtractedPage
	for _, p := range pages {
		prefix := filepath.Join(renderDir, "pg")
		if err := RunPdfToPPM(ctx, pdftoppm, workDir, pdfPath, p.Numero, prefix); err != nil {
			return nil, err
		}
		matches, _ := filepath.Glob(prefix + "*.png")
		if len(matches) == 0 {
			return nil, fmt.Errorf("pdftoppm não gerou a imagem da página %d", p.Numero)
		}
		pngPath := filepath.Join(pagesDir, fmt.Sprintf("%d.png", p.Numero))
		if err := os.Rename(matches[0], pngPath); err != nil {
			return nil, err
		}
		w, h, err := PNGSize(pngPath)
		if err != nil {
			return nil, err
		}
		sx := renderDPIFloat()
		sy := sx
		if p.WidthPt > 0 {
			sx = float64(w) / p.WidthPt
		}
		if p.HeightPt > 0 {
			sy = float64(h) / p.HeightPt
		}
		for i := range p.Words {
			p.Words[i].X0 *= sx
			p.Words[i].X1 *= sx
			p.Words[i].Y0 *= sy
			p.Words[i].Y1 *= sy
		}
		camadaPath := filepath.Join(pagesDir, fmt.Sprintf("%d.camada.json", p.Numero))
		data, _ := json.MarshalIndent(p.Words, "", "  ")
		if err := os.WriteFile(camadaPath, data, 0o644); err != nil {
			return nil, err
		}
		layoutPath := filepath.Join(pagesDir, fmt.Sprintf("%d.layout.json", p.Numero))
		linhas := layoutByPage[p.Numero]
		if linhas == nil {
			linhas = LinesFromWords(p.Words)
		}
		layoutData, _ := json.MarshalIndent(linhas, "", "  ")
		if err := os.WriteFile(layoutPath, layoutData, 0o644); err != nil {
			return nil, err
		}
		result = append(result, ExtractedPage{
			Numero:     p.Numero,
			PNGPath:    pngPath,
			CamadaPath: camadaPath,
			Words:      p.Words,
			WidthPx:    w,
			HeightPx:   h,
			WidthPt:    p.WidthPt,
			HeightPt:   p.HeightPt,
		})
	}
	return result, nil
}
