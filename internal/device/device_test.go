package device

import (
	"image"
	"testing"
)

// A pixel drawn on the landscape canvas, once rotated onto the panel, must map back
// to the same landscape point when touched there.
func TestToLandscapeUndoesRotate(t *testing.T) {
	const w, h = 1648, 1236
	for _, o := range []string{"usb-right", "usb-left"} {
		for _, pt := range []image.Point{{0, 0}, {w - 1, 0}, {100, 900}, {w - 1, h - 1}} {
			src := image.NewGray(image.Rect(0, 0, w, h))
			src.Pix[pt.Y*src.Stride+pt.X] = 255
			dst := Rotate(src, o)
			var found Point
			for i, v := range dst.Pix {
				if v == 255 {
					found = Point{i % dst.Stride, i / dst.Stride}
				}
			}
			if x, y := ToLandscape(found, o, w, h); x != pt.X || y != pt.Y {
				t.Errorf("%s: %v -> panel %v -> %d,%d", o, pt, found, x, y)
			}
		}
	}
}
