package docx

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

type LinhaInput struct {
	Texto   string
	Tamanho float64
	Familia string
	Cor     string
}

type PageInput struct {
	Linhas []LinhaInput
}

type NotaInput struct {
	Pagina int
	Texto  string
}

func Build(outPath string, pages []PageInput, notas []NotaInput) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)

	if err := writeZipString(zw, "[Content_Types].xml", contentTypesXML); err != nil {
		return err
	}
	if err := writeZipString(zw, "_rels/.rels", rootRelsXML); err != nil {
		return err
	}
	if err := writeZipString(zw, "word/styles.xml", stylesXML); err != nil {
		return err
	}
	if err := writeZipString(zw, "word/_rels/document.xml.rels", documentRelsXML); err != nil {
		return err
	}
	if err := writeZipString(zw, "word/document.xml", documentXML(pages, notas)); err != nil {
		return err
	}
	return zw.Close()
}

func writeZipString(zw *zip.Writer, name, content string) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, content)
	return err
}

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/></Types>`

const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`

const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:sz w:val="22"/></w:rPr></w:rPrDefault><w:pPrDefault><w:spacing w:after="120" w:line="276" w:lineRule="auto"/></w:pPrDefault></w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style></w:styles>`

const documentRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`

var corValida = regexp.MustCompile(`^#?[0-9a-fA-F]{6}$`)

func documentXML(pages []PageInput, notas []NotaInput) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)

	for i, p := range pages {
		if i > 0 {
			b.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
		}
		for _, linha := range p.Linhas {
			texto := strings.TrimSpace(linha.Texto)
			if texto == "" {
				continue
			}
			b.WriteString(`<w:p><w:pPr><w:spacing w:before="20" w:after="20" w:line="240" w:lineRule="auto"/></w:pPr><w:r><w:rPr>`)
			if linha.Familia != "" {
				fmt.Fprintf(&b, `<w:rFonts w:ascii="%s" w:hAnsi="%s"/>`, escapeXML(linha.Familia), escapeXML(linha.Familia))
			}
			if linha.Tamanho > 0 {
				fmt.Fprintf(&b, `<w:sz w:val="%d"/>`, int(linha.Tamanho*2+0.5))
			}
			if corValida.MatchString(linha.Cor) {
				fmt.Fprintf(&b, `<w:color w:val="%s"/>`, strings.TrimPrefix(linha.Cor, "#"))
			}
			b.WriteString(`</w:rPr><w:t xml:space="preserve">`)
			b.WriteString(escapeXML(texto))
			b.WriteString(`</w:t></w:r></w:p>`)
		}
	}

	if len(notas) > 0 {
		b.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
		b.WriteString(`<w:p><w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">Notas</w:t></w:r></w:p>`)
		for _, n := range notas {
			fmt.Fprintf(&b, `<w:p><w:r><w:t xml:space="preserve">Página %d: %s</w:t></w:r></w:p>`, n.Pagina, escapeXML(n.Texto))
		}
	}

	b.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr></w:body></w:document>`)
	return b.String()
}

func escapeXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}
