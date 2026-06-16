package svg

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
)

var textPlaceholders = []string{
	"nama_instansi", "nama_kartu", "uid_kartu",
	"nama_user", "username", "hp_instansi",
	"ig_instansi", "email_instansi", "web_instansi",
	"alamat_instansi",
}

type Template struct {
	source string
}

func Compile(svgData []byte) *Template {
	return &Template{source: string(svgData)}
}

func EncodeImages(images map[string][]byte) map[string]string {
	result := make(map[string]string, len(images))
	for id, data := range images {
		if len(data) == 0 {
			continue
		}
		b64 := base64.StdEncoding.EncodeToString(data)
		result[id] = "data:" + detectMime(data) + ";base64," + b64
	}
	return result
}

func (t *Template) Inject(row map[string]string, globalURIs map[string]string, rowImages map[string][]byte) ([]byte, error) {
	pairs := make([]string, 0, len(textPlaceholders)*2)
	for _, key := range textPlaceholders {
		val := row[key]
		if val == "" {
			val = row[strings.ReplaceAll(key, "_", "")]
		}
		pairs = append(pairs, "%VAR_"+key+"%", escXML(val))
	}
	content := strings.NewReplacer(pairs...).Replace(t.source)

	for id, uri := range globalURIs {
		content = embedImageInSVG(content, id, uri)
	}

	for id, imgData := range rowImages {
		if len(imgData) == 0 {
			continue
		}
		b64 := base64.StdEncoding.EncodeToString(imgData)
		dataURI := "data:" + detectMime(imgData) + ";base64," + b64
		content = embedImageInSVG(content, id, dataURI)
	}

	return []byte(content), nil
}

func detectMime(data []byte) string {
	t := http.DetectContentType(data)
	if strings.HasPrefix(t, "image/") {
		return t
	}
	return "image/png"
}

func embedImageInSVG(svg, id, dataURI string) string {
	idAttr := fmt.Sprintf(`id="%s"`, id)
	for _, openTag := range []string{"<image", "<rect"} {
		idx := 0
		for {
			pos := strings.Index(svg[idx:], openTag)
			if pos == -1 {
				break
			}
			pos += idx
			after := pos + len(openTag)
			if after >= len(svg) || !isWS(rune(svg[after])) {
				idx = pos + 1
				continue
			}
			end := findTagEnd(svg[pos:])
			if end == -1 {
				break
			}
			end += pos
			tagContent := svg[pos:end]
			if !strings.Contains(tagContent, idAttr) {
				idx = pos + 1
				continue
			}
			var newTag string
			if openTag == "<image" {
				newTag = replAttr(tagContent, "href", dataURI)
				newTag = replAttr(newTag, "xlink:href", dataURI)
			} else {
				x := exAttr(tagContent, "x")
				y := exAttr(tagContent, "y")
				w := exAttr(tagContent, "width")
				h := exAttr(tagContent, "height")
				newTag = fmt.Sprintf(
					`<image id="%s" x="%s" y="%s" width="%s" height="%s" preserveAspectRatio="xMidYMid slice" href="%s" xlink:href="%s" />`,
					id, x, y, w, h, dataURI, dataURI,
				)
				tail := svg[end:]
				if endR := strings.Index(tail, "</rect>"); endR != -1 &&
					strings.TrimSpace(tail[:min(endR+1, len(tail))]) == "" {
					end += endR + len("</rect>")
				}
			}
			return svg[:pos] + newTag + svg[end:]
		}
	}
	return svg
}

func isWS(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

func findTagEnd(s string) int {
	inQ := rune(0)
	for i, r := range s {
		if inQ != 0 {
			if r == inQ {
				inQ = 0
			}
			continue
		}
		if r == '"' || r == '\'' {
			inQ = r
			continue
		}
		if r == '>' {
			return i + 1
		}
	}
	return -1
}

func replAttr(tag, attr, val string) string {
	for _, q := range []string{`"`, `'`} {
		pat := attr + `=` + q
		s := strings.Index(tag, pat)
		if s == -1 {
			continue
		}
		vs := s + len(pat)
		ve := strings.Index(tag[vs:], q)
		if ve == -1 {
			continue
		}
		return tag[:s] + attr + `="` + val + `"` + tag[vs+ve+1:]
	}
	return strings.TrimSuffix(tag, ">") + ` ` + attr + `="` + val + `">`
}

func exAttr(tag, attr string) string {
	for _, q := range []string{`"`, `'`} {
		pat := attr + `=` + q
		s := strings.Index(tag, pat)
		if s == -1 {
			continue
		}
		s += len(pat)
		e := strings.Index(tag[s:], q)
		if e == -1 {
			continue
		}
		return tag[s : s+e]
	}
	return ""
}

func escXML(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
