package docx

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildProducesValidDocx(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "teste.docx")
	pages := []PageInput{
		{Linhas: []LinhaInput{
			{Texto: "Olá mundo & <bom> teste", Tamanho: 13, Familia: "Helvetica", Cor: "#000000"},
			{Texto: "Linha sem estilo"},
		}},
		{Linhas: []LinhaInput{
			{Texto: "Segunda página"},
		}},
	}
	notas := []NotaInput{
		{Pagina: 1, Texto: "Nota da página 1"},
		{Pagina: 2, Texto: "Nota <com> & detalhes"},
	}
	if err := Build(out, pages, notas); err != nil {
		t.Fatalf("Build: %v", err)
	}

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("não é um zip válido: %v", err)
	}
	defer zr.Close()

	found := map[string]bool{}
	for _, f := range zr.File {
		found[f.Name] = true
	}
	wantEntries := []string{
		"[Content_Types].xml", "_rels/.rels",
		"word/document.xml", "word/_rels/document.xml.rels", "word/styles.xml",
	}
	for _, e := range wantEntries {
		if !found[e] {
			t.Errorf("entrada %q ausente do docx", e)
		}
	}
	if found["word/media/pagina-1.png"] {
		t.Errorf("conteúdo não pode ser imagem: word/media/pagina-1.png presente")
	}

	docBytes, err := readZipEntry(zr, "word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(docBytes)

	dec := xml.NewDecoder(strings.NewReader(doc))
	for {
		if _, err := dec.Token(); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("document.xml não é XML bem formado: %v", err)
		}
	}

	if strings.Count(doc, "<w:vanish/>") != 0 {
		t.Errorf("texto invisível não pode ser usado como conteúdo; veio %d <w:vanish/>", strings.Count(doc, "<w:vanish/>"))
	}
	if strings.Contains(doc, "<pic:pic>") {
		t.Errorf("conteúdo não pode ser imagem colada")
	}
	if !strings.Contains(doc, "Olá mundo &amp; &lt;bom&gt; teste") {
		t.Errorf("linha da página 1 não escapada/gravada como texto editável: %s", doc)
	}
	if !strings.Contains(doc, `<w:rFonts w:ascii="Helvetica" w:hAnsi="Helvetica"/>`) {
		t.Errorf("fonte da linha não preservada")
	}
	if !strings.Contains(doc, `<w:sz w:val="26"/>`) {
		t.Errorf("tamanho 13pt deveria virar sz=26 (meios-pontos)")
	}
	if strings.Count(doc, `<w:br w:type="page"/>`) != 2 {
		t.Errorf("esperado 2 quebras de página (entre páginas e antes das notas), veio %d", strings.Count(doc, `<w:br w:type="page"/>`))
	}
	if !strings.Contains(doc, "Página 1: Nota da página 1") {
		t.Errorf("nota da página 1 ausente")
	}
	if !strings.Contains(doc, "Página 2: Nota &lt;com&gt; &amp; detalhes") {
		t.Errorf("nota da página 2 ausente ou mal escapada")
	}
}

func readZipEntry(zr *zip.ReadCloser, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, os.ErrNotExist
}
