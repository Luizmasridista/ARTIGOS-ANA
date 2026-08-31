package pdf

import (
	"strings"
	"testing"
)

const fixtureBBox = `<!DOCTYPE html>
<html>
<head>
<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
</head>
<body>
<doc>
<page width="595.276000" height="841.890000">
<flow>
<block>
<line>
<word xMin="56.692000" yMin="76.922000" xMax="110.776000" yMax="93.518000">Olá</word>
<word xMin="115.300000" yMin="76.922000" xMax="171.010000" yMax="93.518000">mundo!</word>
<word xMin="200.000000" yMin="100.000000" xMax="250.000000" yMax="115.000000">A &amp; B</word>
</line>
</block>
</flow>
</page>
<page width="595.276000" height="841.890000">
<flow><block><line>
<word xMin="10.000000" yMin="20.000000" xMax="40.000000" yMax="32.800000">café</word>
</line></block></flow>
</page>
</doc>
</body>
</html>`

func TestParseBBox(t *testing.T) {
	pages, err := ParseBBox(strings.NewReader(fixtureBBox))
	if err != nil {
		t.Fatalf("ParseBBox: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("esperado 2 páginas, veio %d", len(pages))
	}
	if pages[0].Numero != 1 || pages[1].Numero != 2 {
		t.Fatalf("numeração errada: %d, %d", pages[0].Numero, pages[1].Numero)
	}
	if pages[0].WidthPt != 595.276 || pages[0].HeightPt != 841.89 {
		t.Fatalf("dimensões da página 1 erradas: %v x %v", pages[0].WidthPt, pages[0].HeightPt)
	}
	words := pages[0].Words
	if len(words) != 3 {
		t.Fatalf("esperado 3 palavras na página 1, veio %d", len(words))
	}
	if words[0].Texto != "Olá" {
		t.Fatalf("texto da palavra 0 errado: %q", words[0].Texto)
	}
	if words[0].X0 != 56.692 || words[0].Y0 != 76.922 || words[0].X1 != 110.776 || words[0].Y1 != 93.518 {
		t.Fatalf("coordenadas da palavra 0 erradas: %+v", words[0])
	}
	if words[2].Texto != "A & B" {
		t.Fatalf("entidade HTML não decodificada: %q", words[2].Texto)
	}
	if pages[1].Words[0].Texto != "café" {
		t.Fatalf("acento na página 2 errado: %q", pages[1].Words[0].Texto)
	}
}

func TestParseBBoxEmpty(t *testing.T) {
	pages, err := ParseBBox(strings.NewReader("<doc><page width=\"10\" height=\"20\"></page></doc>"))
	if err != nil {
		t.Fatalf("ParseBBox: %v", err)
	}
	if len(pages) != 1 || len(pages[0].Words) != 0 {
		t.Fatalf("página vazia mal parseada: %+v", pages)
	}
}
