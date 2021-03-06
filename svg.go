package diagram

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image/color"
	"io"
	"math"
	"sort"
)

type SVG struct {
	Style string
	svgContext
}

type svgContext struct {
	index int
	clip  bool
	// bounds relative to parent
	bounds   Rect
	elements []svgElement
	layers   []*svgContext
}

type svgElement struct {
	// style
	style Style
	// line
	points []Point
	// text
	text   string
	origin Point
	// context
	context *svgContext
}

func NewSVG(width, height Length) *SVG {
	svg := &SVG{}
	svg.Style = `text { text-shadow: -1px -1px 0 rgba(255,255,255,0.5),	1px -1px 0 rgba(255,255,255,0.5), 1px  1px 0 rgba(255,255,255,0.5), -1px  1px 0 rgba(255,255,255,0.5); }`
	svg.bounds.Max.X = width
	svg.bounds.Max.Y = height
	return svg
}

func (svg *SVG) Bytes() []byte {
	var buffer bytes.Buffer
	svg.WriteTo(&buffer)
	return buffer.Bytes()
}

func (svg *svgContext) Bounds() Rect { return svg.bounds.Zero() }
func (svg *svgContext) Size() Point  { return svg.bounds.Size() }

func (svg *svgContext) context(r Rect, clip bool) Canvas {
	element := svgElement{}
	element.context = &svgContext{}
	element.context.clip = clip
	element.context.bounds = r
	svg.elements = append(svg.elements, element)
	return element.context
}

func (svg *svgContext) Context(r Rect) Canvas { return svg.context(r, false) }
func (svg *svgContext) Clip(r Rect) Canvas    { return svg.context(r, true) }

func (svg *svgContext) Layer(index int) Canvas {
	if index == 0 {
		return svg
	}

	i := sort.Search(len(svg.layers), func(i int) bool {
		return svg.layers[i].index > index
	})
	if i < len(svg.layers) && svg.layers[i].index == index {
		return svg.layers[i]
	}

	layer := &svgContext{}
	layer.index = index
	layer.bounds = svg.bounds.Zero()

	svg.layers = append(svg.layers, layer)
	copy(svg.layers[i+1:], svg.layers[i:])
	svg.layers[i] = layer
	return layer

}

func (svg *svgContext) Text(text string, at Point, style *Style) {
	style.mustExist()
	svg.elements = append(svg.elements, svgElement{
		text:   text,
		origin: at,
		style:  *style,
	})
}

func (svg *svgContext) Poly(points []Point, style *Style) {
	style.mustExist()
	svg.elements = append(svg.elements, svgElement{
		points: points,
		style:  *style,
	})
}

func (svg *svgContext) Rect(r Rect, style *Style) {
	svg.Poly(r.Points(), style)
}

func (svg *SVG) WriteTo(dst io.Writer) (n int64, err error) {
	w := &writer{}
	w.Writer = dst

	id := 0

	// svg wrapper
	w.Print(`<?xml version="1.0" standalone="no"?>`)
	w.Print(`<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.0//EN" "http://www.w3.org/TR/2001/REC-SVG-20010904/DTD/svg10.dtd">`)
	size := svg.bounds.Size()
	w.Print(`<svg xmlns='http://www.w3.org/2000/svg' xmlns:loov='http://www.loov.io' width='%vpx' height='%vpx'>`, size.X, size.Y)
	defer w.Print(`</svg>`)

	if svg.Style != "" {
		// TODO: escape CDATA
		w.Print(`<style>/* <![CDATA[ */ %v /* ]]> */ </style>`, svg.Style)
	}

	w.Print(`<g transform='translate(0.5, 0.5)'>`)
	defer w.Print(`</g>`)

	var writeLayer func(svg *svgContext)
	var writeElement func(svg *svgContext, el *svgElement)

	writeLayer = func(svg *svgContext) {
		if svg.clip {
			id++
			size := svg.bounds.Size()
			w.Print(`<clipPath id='clip%v'><rect x='0' y='0' width='%v' height='%v' /></clipPath>`, id, size.X, size.Y)
		}

		w.Printf(`<g`)
		w.Printf(` loov:index='%v'`, svg.index)
		if svg.bounds.Min.X != 0 || svg.bounds.Min.Y != 0 {
			w.Printf(` transform='translate(%.2f %.2f)'`, svg.bounds.Min.X, svg.bounds.Min.Y)
		}
		if svg.clip {
			w.Printf(` clip-path='url(#clip%v)'`, id)
		}

		w.Print(">")
		defer w.Print(`</g>`)

		after := 0
		for i, layer := range svg.layers {
			if layer.index >= 0 {
				after = i
				break
			}
			writeLayer(layer)
		}

		if len(svg.elements) > 0 {
			if len(svg.layers) > 0 {
				w.Print("<g>")
			}
			for i := range svg.elements {
				writeElement(svg, &svg.elements[i])
			}
			if len(svg.layers) > 0 {
				w.Print("</g>")
			}
		}

		for _, layer := range svg.layers[after:] {
			writeLayer(layer)
		}
	}

	writeElement = func(svg *svgContext, el *svgElement) {
		if len(el.points) > 0 {
			w.Printf(`<polyline `)
			w.writePolyStyle(&el.style)
			w.Printf(` points='`)
			for _, p := range el.points {
				w.Printf(`%.2f,%.2f `, p.X, p.Y)
			}
			if el.style.Hint != "" {
				w.Print(`' >`)
				w.Printf(`<title>'`)
				xml.EscapeText(w, []byte(el.style.Hint))
				w.Printf(`</title>`)
				w.Print(`'</polyline>`)
			} else {
				w.Print(`' />`)
			}
		}
		if el.text != "" {
			w.Printf(`<g transform='translate(%.2f,%2.f)'>`, el.origin.X, el.origin.Y)
			w.Printf(`<text `)
			w.writeTextStyle(&el.style)
			w.Printf(`>`)
			if el.style.Hint != "" {
				w.Printf(`<title>'`)
				xml.EscapeText(w, []byte(el.style.Hint))
				w.Printf(`</title>`)
			}
			xml.EscapeText(w, []byte(el.text))
			w.Print(`</text></g>`)
		}
		if el.context != nil {
			writeLayer(el.context)
		}
	}

	writeLayer(&svg.svgContext)

	return w.total, w.err
}

func (w *writer) writeTextStyle(style *Style) {
	// TODO: merge with writePolyStyle
	if style.Class != "" {
		w.Printf(` class='`)
		xml.EscapeText(w, []byte(style.Class))
		w.Printf(`'`)
	}

	if style.Rotation != 0 {
		w.Printf(`transform="rotate(%.2f)" `, style.Rotation*180/math.Pi)
	}

	if style.Origin.X == 0 {
		w.Printf(`text-anchor="middle" `)
	} else if style.Origin.X == 1 {
		w.Printf(`text-anchor="end" `)
	} else if style.Origin.X == -1 {
		w.Printf(`text-anchor="start" `)
	}

	if style.Origin.Y == 0 {
		w.Printf(`alignment-baseline="middle" `)
	} else if style.Origin.Y == 1 {
		w.Printf(`alignment-baseline="baseline" `)
	} else if style.Origin.Y == -1 {
		w.Printf(`alignment-baseline="hanging" `)
	}

	if style.Font == "" && style.Size == 0 && style.Stroke == nil && style.Fill == nil {
		return
	}

	w.Printf(` style='`)
	defer w.Printf(`' `)

	if style.Font != "" {
		w.Printf(`font-family: "`)
		// TODO, verify escaping
		xml.EscapeText(w, []byte(style.Font))
		w.Printf(`";`)
	}
	if style.Size != 0 {
		w.Printf(`font-size: %vpx;`, style.Size)
	}

	if style.Stroke != nil {
		w.Printf(`color: %v;`, convertColorToHex(style.Stroke))
	}
	if style.Fill != nil {
		w.Printf(`fill: %v;`, convertColorToHex(style.Fill))
	}

}

func (w *writer) writePolyStyle(style *Style) {
	if style.Class != "" {
		w.Printf(` class='`)
		xml.EscapeText(w, []byte(style.Class))
		w.Printf(`'`)
	}

	if style.Size == 0 && style.Stroke == nil && style.Fill == nil && len(style.Dash) == 0 {
		return
	}

	w.Printf(` style='`)
	defer w.Printf(`'`)

	if style.Stroke != nil {
		w.Printf(`stroke: %v;`, convertColorToHex(style.Stroke))
	} else {
		w.Printf(`stroke: transparent;`)
	}

	if style.Fill != nil {
		w.Printf(`fill: %v;`, convertColorToHex(style.Fill))
	} else {
		w.Printf(`fill: transparent;`)
	}

	if len(style.Dash) > 0 {
		w.Printf(`stroke-dasharray:`)
		for _, v := range style.Dash {
			w.Printf(` %v`, v)
		}
		w.Printf(`;`)
	}

	if style.Size != 0 {
		w.Printf(`stroke-width: %vpx;`, style.Size)
	}

	// TODO: dash
}

func convertColorToHex(color color.Color) string {
	r, g, b, a := color.RGBA()
	if a > 0 {
		r, g, b, a = r*0xff/a, g*0xff/a, b*0xff/a, a/0xff
		if r > 0xFF {
			r = 0xFF
		}
		if g > 0xFF {
			g = 0xFF
		}
		if b > 0xFF {
			b = 0xFF
		}
		if a > 0xFF {
			a = 0xFF
		}
	} else {
		r, g, b, a = 0, 0, 0, 0
	}
	hex := r<<24 | g<<16 | b<<8 | a
	return fmt.Sprintf("#%08x", hex)
}

type writer struct {
	io.Writer
	total int64
	err   error
}

func (w *writer) Errored() bool { return w.err != nil }

func (w *writer) Write(data []byte) (int, error) {
	if w.Errored() {
		return 0, nil
	}
	n, err := w.Writer.Write(data)
	w.total += int64(n)
	if err != nil {
		w.err = err
	}
	return n, nil
}

func (w *writer) Print(format string, args ...interface{})  { fmt.Fprintf(w, format+"\n", args...) }
func (w *writer) Printf(format string, args ...interface{}) { fmt.Fprintf(w, format, args...) }
