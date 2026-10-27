package render

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// E-ink panels show 16 grey levels; stick to multiples of 0x11 so nothing gets dithered.
const (
	cBlack = 0x00
	cDark  = 0x44
	cMid   = 0x88
	cLight = 0xDD
	cWhite = 0xFF
)

type Align int

const (
	Left Align = iota
	Center
	Right
)

type Weight int

const (
	Regular Weight = iota
	Medium
	Bold
)

var (
	parsedFonts = map[Weight]*opentype.Font{}
	faceCache   = map[string]font.Face{}
)

func Face(w Weight, px float64) font.Face {
	key := fmt.Sprintf("%d/%.1f", w, px)
	if f, ok := faceCache[key]; ok {
		return f
	}
	otf, ok := parsedFonts[w]
	if !ok {
		data := map[Weight][]byte{Regular: goregular.TTF, Medium: gomedium.TTF, Bold: gobold.TTF}[w]
		var err error
		if otf, err = opentype.Parse(data); err != nil {
			panic(err)
		}
		parsedFonts[w] = otf
	}
	f, err := opentype.NewFace(otf, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	faceCache[key] = f
	return f
}

type Canvas struct {
	Img  *image.Gray
	W, H int
}

func NewCanvas(w, h int) *Canvas {
	c := &Canvas{Img: image.NewGray(image.Rect(0, 0, w, h)), W: w, H: h}
	for i := range c.Img.Pix {
		c.Img.Pix[i] = cWhite
	}
	return c
}

func (c *Canvas) set(x, y int, v uint8) {
	if x >= 0 && y >= 0 && x < c.W && y < c.H {
		c.Img.Pix[y*c.Img.Stride+x] = v
	}
}

func (c *Canvas) Rect(x, y, w, h int, v uint8) {
	x0, y0 := max(x, 0), max(y, 0)
	x1, y1 := min(x+w, c.W), min(y+h, c.H)
	for yy := y0; yy < y1; yy++ {
		row := c.Img.Pix[yy*c.Img.Stride:]
		for xx := x0; xx < x1; xx++ {
			row[xx] = v
		}
	}
}

func (c *Canvas) StrokeRect(x, y, w, h, t int, v uint8) {
	c.Rect(x, y, w, t, v)
	c.Rect(x, y+h-t, w, t, v)
	c.Rect(x, y, t, h, v)
	c.Rect(x+w-t, y, t, h, v)
}

// RoundRect fills a rectangle with corners of radius r.
func (c *Canvas) RoundRect(x, y, w, h, r int, v uint8) {
	r = min(r, w/2, h/2)
	for yy := 0; yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			cx, cy := -1, -1
			switch {
			case xx < r && yy < r:
				cx, cy = r, r
			case xx >= w-r && yy < r:
				cx, cy = w-r-1, r
			case xx < r && yy >= h-r:
				cx, cy = r, h-r-1
			case xx >= w-r && yy >= h-r:
				cx, cy = w-r-1, h-r-1
			}
			if cx >= 0 {
				dx, dy := float64(xx-cx), float64(yy-cy)
				if dx*dx+dy*dy > float64(r*r) {
					continue
				}
			}
			c.set(x+xx, y+yy, v)
		}
	}
}

// Line draws a thick line with a square brush.
func (c *Canvas) Line(x0, y0, x1, y1 float64, t int, v uint8) {
	steps := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))) + 1
	for i := 0; i <= steps; i++ {
		f := float64(i) / float64(steps)
		x := x0 + (x1-x0)*f
		y := y0 + (y1-y0)*f
		c.Rect(int(math.Round(x))-t/2, int(math.Round(y))-t/2, t, t, v)
	}
}

func (c *Canvas) DashH(x0, x1, y, t, dash, gap int, v uint8) {
	for x := x0; x < x1; x += dash + gap {
		c.Rect(x, y, min(dash, x1-x), t, v)
	}
}

// Text draws s with its baseline at y and returns the drawn width.
func (c *Canvas) Text(s string, f font.Face, x, y int, a Align, v uint8) int {
	w := font.MeasureString(f, s).Ceil()
	switch a {
	case Center:
		x -= w / 2
	case Right:
		x -= w
	}
	d := &font.Drawer{Dst: c.Img, Src: image.NewUniform(color.Gray{Y: v}), Face: f, Dot: fixed.P(x, y)}
	d.DrawString(s)
	return w
}

func TextWidth(f font.Face, s string) int { return font.MeasureString(f, s).Ceil() }

// Fit shrinks the font size until s is at most maxW wide.
func FitFace(w Weight, px float64, s string, maxW int) font.Face {
	for px > 12 {
		f := Face(w, px)
		if TextWidth(f, s) <= maxW {
			return f
		}
		px -= 2
	}
	return Face(w, px)
}
