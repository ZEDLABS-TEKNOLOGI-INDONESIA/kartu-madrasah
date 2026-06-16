package svg

import (
	"encoding/xml"
	"math"
	"strconv"
	"strings"
)

var AllowedIDs = map[string]bool{
	"logo_instansi": true,
	"foto_user":     true,
	"qr_user":       true,
}

type SVGNode struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Nodes   []SVGNode  `xml:",any"`
}

type SVGMatrix struct{ a, b, c, d, e, f float64 }

func svgId() SVGMatrix { return SVGMatrix{1, 0, 0, 1, 0, 0} }

func svgMul(m1, m2 SVGMatrix) SVGMatrix {
	return SVGMatrix{
		a: m2.a*m1.a + m2.c*m1.b, b: m2.b*m1.a + m2.d*m1.b,
		c: m2.a*m1.c + m2.c*m1.d, d: m2.b*m1.c + m2.d*m1.d,
		e: m2.a*m1.e + m2.c*m1.f + m2.e, f: m2.b*m1.e + m2.d*m1.f + m2.f,
	}
}

func (m SVGMatrix) apply(x, y float64) (float64, float64) {
	return m.a*x + m.c*y + m.e, m.b*x + m.d*y + m.f
}

func parseTransform(s string) SVGMatrix {
	res := svgId()
	s = strings.TrimSpace(s)
	for len(s) > 0 {
		op, cl := strings.Index(s, "("), strings.Index(s, ")")
		if op == -1 || cl == -1 {
			break
		}
		name := strings.TrimSpace(s[:op])
		var args []float64
		for _, p := range strings.FieldsFunc(s[op+1:cl], func(r rune) bool {
			return r == ',' || r == ' '
		}) {
			if v, err := strconv.ParseFloat(p, 64); err == nil {
				args = append(args, v)
			}
		}
		var m SVGMatrix
		switch name {
		case "translate":
			tx, ty := args[0], 0.0
			if len(args) > 1 {
				ty = args[1]
			}
			m = SVGMatrix{1, 0, 0, 1, tx, ty}
		case "scale":
			sx, sy := args[0], args[0]
			if len(args) > 1 {
				sy = args[1]
			}
			m = SVGMatrix{sx, 0, 0, sy, 0, 0}
		case "matrix":
			if len(args) == 6 {
				m = SVGMatrix{args[0], args[1], args[2], args[3], args[4], args[5]}
			} else {
				m = svgId()
			}
		case "rotate":
			th := args[0] * math.Pi / 180
			co, si := math.Cos(th), math.Sin(th)
			rot := SVGMatrix{co, si, -si, co, 0, 0}
			if len(args) == 3 {
				cx, cy := args[1], args[2]
				m = svgMul(svgMul(SVGMatrix{1, 0, 0, 1, -cx, -cy}, rot), SVGMatrix{1, 0, 0, 1, cx, cy})
			} else {
				m = rot
			}
		default:
			m = svgId()
		}
		res = svgMul(res, m)
		s = strings.TrimSpace(s[cl+1:])
	}
	return res
}

func svgAttr(n SVGNode, name string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func svgFloat(s string) float64 {
	s = strings.TrimSuffix(strings.TrimSpace(s), "px")
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func r1(v float64) float64 { return math.Round(v*10) / 10 }

func tBox(m SVGMatrix, x0, y0, x1, y1 float64) (float64, float64, float64, float64) {
	cs := [][2]float64{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range cs {
		px, py := m.apply(c[0], c[1])
		minX, minY = math.Min(minX, px), math.Min(minY, py)
		maxX, maxY = math.Max(maxX, px), math.Max(maxY, py)
	}
	return minX, minY, maxX, maxY
}

type Extracted map[string]interface{}

func walkSVG(n SVGNode, pm SVGMatrix, res map[string]Extracted) {
	m := pm
	if t := svgAttr(n, "transform"); t != "" {
		m = svgMul(pm, parseTransform(t))
	}
	id, tag := svgAttr(n, "id"), n.XMLName.Local
	if id != "" && AllowedIDs[id] {
		switch tag {
		case "rect", "image":
			x, y := svgFloat(svgAttr(n, "x")), svgFloat(svgAttr(n, "y"))
			w, h := svgFloat(svgAttr(n, "width")), svgFloat(svgAttr(n, "height"))
			minX, minY, maxX, maxY := tBox(m, x, y, x+w, y+h)
			res[id] = Extracted{
				"type": "rect", "x": r1(minX), "y": r1(minY),
				"width": r1(maxX - minX), "height": r1(maxY - minY),
			}
		case "circle", "ellipse":
			cx, cy := svgFloat(svgAttr(n, "cx")), svgFloat(svgAttr(n, "cy"))
			r := svgFloat(svgAttr(n, "r"))
			if r == 0 {
				r = svgFloat(svgAttr(n, "rx"))
			}
			minX, minY, maxX, maxY := tBox(m, cx-r, cy-r, cx+r, cy+r)
			res[id] = Extracted{
				"type":     "circle",
				"center_x": r1((minX + maxX) / 2),
				"center_y": r1((minY + maxY) / 2),
				"diameter": r1(maxX - minX),
			}
		}
	}
	for _, child := range n.Nodes {
		walkSVG(child, m, res)
	}
}

func ExtractConfig(svgData []byte) (map[string]Extracted, error) {
	var root SVGNode
	if err := xml.Unmarshal(svgData, &root); err != nil {
		return nil, err
	}
	res := make(map[string]Extracted)
	walkSVG(root, svgId(), res)
	return res, nil
}
