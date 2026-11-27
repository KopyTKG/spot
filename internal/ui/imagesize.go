package ui

import (
	"bytes"
	"encoding/xml"
	"image"
	_ "image/gif" // headers only, for DecodeConfig
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// An image is decoded in full before it is scaled down: its dimensions, not its
// size on disk, say how much memory and time that takes.
const (
	maxImageFile   = 8 << 20
	maxImagePixels = 4096 * 4096
)

// imageFits reports whether an icon named by path is safe to hand to GTK. Only
// the header is read, here: GTK's own question about an image's size renders an
// SVG first, which for a huge one takes minutes on the UI thread. Formats not
// listed are refused.
func imageFits(path string) bool {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxImageFile {
		return false
	}
	b, err := readSmall(path, maxImageFile)
	if err != nil {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".svg":
		w, h, ok := svgSize(b)
		return ok && w*h <= maxImagePixels
	case ".xpm":
		return true // every pixel is written out, so the file limit covers it
	}
	c, _, err := image.DecodeConfig(bytes.NewReader(b))
	return err == nil && c.Width > 0 && c.Height > 0 && float64(c.Width)*float64(c.Height) <= maxImagePixels
}

// svgSize is the size the root element asks to be drawn at; 0 when it says
// nothing, which renderers treat as small.
func svgSize(b []byte) (w, h float64, ok bool) {
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = false
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	for {
		tok, err := d.Token()
		if err != nil {
			return 0, 0, false
		}
		root, is := tok.(xml.StartElement)
		if !is {
			continue
		}
		if root.Name.Local != "svg" {
			return 0, 0, false
		}
		var boxW, boxH float64
		for _, a := range root.Attr {
			switch a.Name.Local {
			case "width":
				w = svgLength(a.Value)
			case "height":
				h = svgLength(a.Value)
			case "viewBox":
				f := strings.FieldsFunc(a.Value, func(r rune) bool { return strings.ContainsRune(" ,\t\r\n", r) })
				if len(f) == 4 {
					boxW, _ = strconv.ParseFloat(f[2], 64)
					boxH, _ = strconv.ParseFloat(f[3], 64)
				}
			}
		}
		if w == 0 {
			w = boxW
		}
		if h == 0 {
			h = boxH
		}
		return w, h, w >= 0 && h >= 0
	}
}

// svgLength is a length in pixels; 0 for a percentage or what is not understood.
func svgLength(s string) float64 {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		return 0
	}
	scale := 1.0
	for unit, px := range map[string]float64{"px": 1, "pt": 4.0 / 3, "pc": 16, "in": 96, "cm": 96 / 2.54, "mm": 96 / 25.4, "em": 16, "ex": 8} {
		if v, found := strings.CutSuffix(s, unit); found {
			s, scale = v, px
			break
		}
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v * scale
}
