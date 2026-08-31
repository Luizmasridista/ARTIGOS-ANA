package pdf

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

type LayoutLine struct {
	Texto   string  `json:"texto"`
	Tamanho float64 `json:"tamanho,omitempty"`
	Familia string  `json:"familia,omitempty"`
	Cor     string  `json:"cor,omitempty"`
}

func ParseBBoxLayout(r io.Reader) (map[int][]LayoutLine, error) {
	z := html.NewTokenizer(r)
	pages := map[int][]LayoutLine{}
	page := 0
	var cur *LayoutLine
	var fontSize float64
	var family, color string
	inWord := false
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if z.Err() == io.EOF {
				return pages, nil
			}
			return nil, fmt.Errorf("bbox-layout inválido: %w", z.Err())
		case html.StartTagToken:
			tag, hasAttr := z.TagName()
			switch string(tag) {
			case "page":
				page++
			case "line":
				if page > 0 {
					cur = &LayoutLine{Familia: family, Tamanho: fontSize, Cor: color}
				}
			case "fontspec":
				if hasAttr {
					for {
						k, v, more := z.TagAttr()
						switch string(k) {
						case "size":
							fontSize, _ = strconv.ParseFloat(string(v), 64)
						case "family":
							family = cleanFontFamily(string(v))
						case "color":
							color = string(v)
						}
						if !more {
							break
						}
					}
				}
				if cur != nil {
					cur.Familia = family
					cur.Tamanho = fontSize
					cur.Cor = color
				}
			case "word":
				inWord = true
			}
		case html.EndTagToken:
			tag, _ := z.TagName()
			switch string(tag) {
			case "word":
				inWord = false
				if cur != nil {
					cur.Texto += " "
				}
			case "line":
				if cur != nil && strings.TrimSpace(cur.Texto) != "" {
					cur.Texto = strings.TrimSpace(cur.Texto)
					pages[page] = append(pages[page], *cur)
				}
				cur = nil
			}
		case html.TextToken:
			if inWord && cur != nil {
				cur.Texto += html.UnescapeString(string(z.Text()))
			}
		}
	}
}

func cleanFontFamily(f string) string {
	if i := strings.IndexByte(f, '+'); i >= 0 && i < len(f)-1 {
		f = f[i+1:]
	}
	return strings.TrimSpace(f)
}

func LinesFromWords(words []Word) []LayoutLine {
	sorted := make([]Word, 0, len(words))
	for _, w := range words {
		if w.Texto != "" {
			sorted = append(sorted, w)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		if math.Abs(sorted[i].Y0-sorted[j].Y0) > 4 {
			return sorted[i].Y0 < sorted[j].Y0
		}
		return sorted[i].X0 < sorted[j].X0
	})
	var lines []LayoutLine
	var ys []float64
	for _, w := range sorted {
		altura := math.Max(w.Y1-w.Y0, 1)
		if n := len(lines); n > 0 && math.Abs(w.Y0-ys[n-1]) < altura*0.6 {
			lines[n-1].Texto += " " + w.Texto
			if lines[n-1].Tamanho == 0 {
				lines[n-1].Tamanho = altura
			}
			continue
		}
		lines = append(lines, LayoutLine{Texto: w.Texto, Tamanho: altura})
		ys = append(ys, w.Y0)
	}
	return lines
}
