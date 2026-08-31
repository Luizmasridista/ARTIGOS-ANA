package pdf

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

type Word struct {
	Texto string  `json:"texto"`
	X0    float64 `json:"x0"`
	Y0    float64 `json:"y0"`
	X1    float64 `json:"x1"`
	Y1    float64 `json:"y1"`
}

type Page struct {
	Numero   int
	WidthPt  float64
	HeightPt float64
	Words    []Word
}

func ParseBBox(r io.Reader) ([]Page, error) {
	z := html.NewTokenizer(r)
	var pages []Page
	var cur *Page
	inWord := false
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if z.Err() == io.EOF {
				goto done
			}
			return nil, fmt.Errorf("bbox html inválido: %w", z.Err())
		case html.StartTagToken:
			tag, hasAttr := z.TagName()
			switch string(tag) {
			case "page":
				width, height := 0.0, 0.0
				if hasAttr {
					for {
						k, v, more := z.TagAttr()
						switch string(k) {
						case "width":
							width, _ = strconv.ParseFloat(string(v), 64)
						case "height":
							height, _ = strconv.ParseFloat(string(v), 64)
						}
						if !more {
							break
						}
					}
				}
				pages = append(pages, Page{WidthPt: width, HeightPt: height})
				cur = &pages[len(pages)-1]
			case "word":
				if cur != nil {
					w := Word{}
					if hasAttr {
						for {
							k, v, more := z.TagAttr()
							f, _ := strconv.ParseFloat(string(v), 64)
							switch string(k) {
							case "xmin":
								w.X0 = f
							case "ymin":
								w.Y0 = f
							case "xmax":
								w.X1 = f
							case "ymax":
								w.Y1 = f
							}
							if !more {
								break
							}
						}
					}
					cur.Words = append(cur.Words, w)
					inWord = true
				}
			}
		case html.EndTagToken:
			tag, _ := z.TagName()
			if string(tag) == "word" {
				inWord = false
			}
		case html.TextToken:
			if inWord && cur != nil && len(cur.Words) > 0 {
				text := strings.TrimSpace(html.UnescapeString(string(z.Text())))
				if text != "" {
					cur.Words[len(cur.Words)-1].Texto += text
				}
			}
		}
	}
done:
	for i := range pages {
		pages[i].Numero = i + 1
	}
	return pages, nil
}
