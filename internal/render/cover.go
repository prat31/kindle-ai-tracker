package render

import (
	"image"
	"math"
)

// Cover is the library thumbnail for the scriptlet: the logo (docs/logo.svg) above the
// app's name. It's drawn at twice the size and scaled down, so the edges are smooth.
func Cover(w, h int) *image.Gray {
	c := NewCanvas(2*w, 2*h)
	const ink = 0x22

	// The logo in its own units (docs/logo.svg, moved to start at 0,0).
	s := float64(2*w) * 0.8 / 464
	ox, oy := float64(2*w)*0.1, float64(2*h)*0.2
	px := func(v float64) int { return int(math.Round(v * s)) }
	rr := func(x, y, rw, rh, r float64, v uint8) {
		c.RoundRect(int(ox)+px(x), int(oy)+px(y), px(rw), px(rh), px(r), v)
	}
	rr(0, 0, 464, 304, 52, ink)
	rr(34, 34, 340, 236, 10, cWhite)

	curve := [][2]float64{{64, 178}, {106, 178}, {128, 136}, {172, 126}, {192, 178},
		{222, 178}, {246, 94}, {286, 74}, {306, 178}, {344, 178}}
	for i := 1; i < len(curve); i++ {
		a, b := curve[i-1], curve[i]
		c.roundLine(ox+a[0]*s, oy+a[1]*s, ox+b[0]*s, oy+b[1]*s, 13*s, ink)
	}

	// the usage bar, with the elapsed-time marker
	rr(64-3.5, 214-3.5, 287, 35, 17.5, ink)
	rr(64+3.5, 214+3.5, 273, 21, 10.5, cWhite)
	rr(64, 214, 168, 28, 14, ink)
	rr(270, 202, 14, 52, 4, cWhite)
	rr(274, 202, 6, 52, 3, ink)

	c.Text("AI Dashboard", FitFace(Bold, float64(2*h)*0.1, "AI Dashboard", 2*w*9/10), w, 2*h*76/100, Center, ink)
	c.Text("usage · limits · resets", Face(Regular, float64(2*h)*0.045), w, 2*h*85/100, Center, cMid)

	out := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := c.Img.Pix[2*y*c.Img.Stride+2*x:]
			q := c.Img.Pix[(2*y+1)*c.Img.Stride+2*x:]
			out.Pix[y*out.Stride+x] = uint8((int(p[0]) + int(p[1]) + int(q[0]) + int(q[1]) + 2) / 4)
		}
	}
	return out
}

// roundLine draws a line with a round brush of diameter d, so joins and ends are round.
func (c *Canvas) roundLine(x0, y0, x1, y1, d float64, v uint8) {
	r := d / 2
	steps := int(math.Hypot(x1-x0, y1-y0)) + 1
	for i := 0; i <= steps; i++ {
		f := float64(i) / float64(steps)
		cx, cy := x0+(x1-x0)*f, y0+(y1-y0)*f
		for y := int(cy - r); y <= int(cy+r)+1; y++ {
			for x := int(cx - r); x <= int(cx+r)+1; x++ {
				if dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy; dx*dx+dy*dy <= r*r {
					c.set(x, y, v)
				}
			}
		}
	}
}
