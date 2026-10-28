package device

import (
	"encoding/binary"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Point is a touch position in portrait panel pixels; X is -1 when the touchscreen
// didn't report a position.
type Point struct{ X, Y int }

// Calibration fixes up touchscreens whose axes don't line up with the panel.
type Calibration struct {
	SwapXY  bool `json:"touch_swap_xy"`
	MirrorX bool `json:"touch_mirror_x"`
	MirrorY bool `json:"touch_mirror_y"`
}

// Gesture is a finger lifted before the hold time: a tap or, if it moved far, a swipe.
type Gesture struct {
	Start, End Point
}

type Touch struct {
	Tap   chan Gesture
	Hold  chan Point
	Power chan struct{} // the power button was pressed
}

const holdTime = 2500 * time.Millisecond

// NoTouch never fires; used off-device.
func NoTouch() *Touch {
	return &Touch{Tap: make(chan Gesture), Hold: make(chan Point), Power: make(chan struct{})}
}

type rawKind int

const (
	rawDown rawKind = iota
	rawUp
	rawX // v is the position normalised to 0..1
	rawY
	rawSync  // end of one batch of updates from the touchscreen
	rawPower // power button pressed
)

type rawEvent struct {
	kind rawKind
	v    float64
}

// WatchTouch reads raw Linux input events. A finger lifted within 2.5 s fires Tap with
// where it went down and came up (the caller tells taps from swipes); a press held for
// 2.5 s fires Hold (once; releasing it afterwards fires nothing). panelW×panelH is the
// portrait panel size. The Kindle UI is stopped while we run, so nothing else reacts.
func WatchTouch(cal Calibration, panelW, panelH int) *Touch {
	t := &Touch{Tap: make(chan Gesture, 1), Hold: make(chan Point, 1), Power: make(chan struct{}, 1)}
	paths, _ := filepath.Glob("/dev/input/event*")
	events := make(chan rawEvent, 64)
	for _, p := range paths {
		go readInput(p, events)
	}
	go func() {
		nx, ny := -1.0, -1.0
		point := func() Point {
			if nx < 0 || ny < 0 {
				return Point{-1, -1}
			}
			x, y := nx, ny
			if cal.SwapXY {
				x, y = y, x
			}
			if cal.MirrorX {
				x = 1 - x
			}
			if cal.MirrorY {
				y = 1 - y
			}
			return Point{int(x * float64(panelW-1)), int(y * float64(panelH-1))}
		}
		var downAt time.Time
		var start Point
		down, haveStart := false, false
		timer := time.NewTimer(time.Hour)
		timer.Stop()
		for {
			select {
			case e := <-events:
				switch e.kind {
				case rawX:
					nx = e.v
				case rawY:
					ny = e.v
				case rawDown:
					if !down {
						down, haveStart, downAt = true, false, time.Now()
						timer.Reset(holdTime)
					}
				case rawPower:
					log.Printf("power button")
					select {
					case t.Power <- struct{}{}:
					default:
					}
				case rawSync:
					// The position of a new touch arrives in the same batch as the touch itself.
					if down && !haveStart {
						start, haveStart = point(), true
					}
				case rawUp:
					if down {
						down = false
						timer.Stop()
						if time.Since(downAt) < holdTime {
							g := Gesture{Start: start, End: point()}
							if !haveStart {
								g.Start = g.End
							}
							log.Printf("touch panel (%d,%d) -> (%d,%d)", g.Start.X, g.Start.Y, g.End.X, g.End.Y)
							select {
							case t.Tap <- g:
							default:
							}
						}
					}
				}
			case <-timer.C:
				if down {
					send(t.Hold, point())
				}
			}
		}
	}()
	return t
}

func send(ch chan Point, p Point) {
	select {
	case ch <- p:
	default:
	}
}

const (
	evKey      = 0x01
	evAbs      = 0x03
	btnTouch   = 0x14a
	absX       = 0x00
	absY       = 0x01
	absMTX     = 0x35
	absMTY     = 0x36
	absTrackID = 0x39
	keyPower   = 116
)

func readInput(path string, events chan<- rawEvent) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	// Position ranges; prefer the multitouch axes, fall back to single-touch ones.
	type axis struct {
		code     uint16
		min, max int32
		kind     rawKind
	}
	var axes []axis
	for _, pair := range [][2]int{{absMTX, absMTY}, {absX, absY}} {
		xmin, xmax, okx := absRange(f, pair[0])
		ymin, ymax, oky := absRange(f, pair[1])
		if okx && oky {
			axes = append(axes, axis{uint16(pair[0]), xmin, xmax, rawX}, axis{uint16(pair[1]), ymin, ymax, rawY})
			log.Printf("touch %s: axes %#x/%#x range x %d..%d y %d..%d", path, pair[0], pair[1], xmin, xmax, ymin, ymax)
			if err := grab(f); err != nil {
				log.Printf("touch %s: couldn't grab input: %v", path, err)
			}
			break
		}
	}

	// struct input_event: timeval (2 longs) + u16 type + u16 code + s32 value
	size := 16
	if strconv.IntSize == 64 {
		size = 24
	}
	buf := make([]byte, size)
	for {
		if _, err := readFull(f, buf); err != nil {
			return
		}
		typ := binary.LittleEndian.Uint16(buf[size-8:])
		code := binary.LittleEndian.Uint16(buf[size-6:])
		val := int32(binary.LittleEndian.Uint32(buf[size-4:]))
		switch {
		case typ == 0 && code == 0: // EV_SYN / SYN_REPORT
			events <- rawEvent{kind: rawSync}
		case typ == evKey && code == keyPower && val == 1:
			events <- rawEvent{kind: rawPower}
		case typ == evKey && code == btnTouch:
			events <- rawEvent{kind: downOrUp(val != 0)}
		case typ == evAbs && code == absTrackID:
			events <- rawEvent{kind: downOrUp(val >= 0)}
		case typ == evAbs:
			for _, a := range axes {
				if a.code == code {
					events <- rawEvent{kind: a.kind, v: float64(val-a.min) / float64(a.max-a.min)}
				}
			}
		}
	}
}

func downOrUp(down bool) rawKind {
	if down {
		return rawDown
	}
	return rawUp
}

func readFull(f *os.File, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := f.Read(buf[n:])
		if err != nil {
			return n, err
		}
		n += m
	}
	return n, nil
}
