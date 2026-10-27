// Package device is everything Kindle-specific: pushing images with FBInk,
// reading the touchscreen, Wi-Fi, firewall and battery.
package device

import (
	"image"
	"image/png"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Display rotates the landscape canvas onto the portrait framebuffer and pushes it with FBInk.
type Display struct {
	Orientation string // "usb-right" or "usb-left"
	FBInk       string
	Dir         string // where screen.png is written
	FullEvery   int    // flashing refresh every N updates
	shown       int
}

func (d *Display) OnKindle() bool {
	_, err := os.Stat(d.FBInk)
	return err == nil
}

// ToLandscape maps a point on the portrait panel back to the landscape canvas,
// undoing Rotate for the current orientation.
func ToLandscape(p Point, orientation string, landscapeW, landscapeH int) (x, y int) {
	if orientation == "usb-left" {
		return landscapeW - 1 - p.Y, p.X
	}
	return p.Y, landscapeH - 1 - p.X
}

func (d *Display) Show(landscape *image.Gray, forceFull bool) error {
	path := filepath.Join(d.Dir, "screen.png")
	if err := WritePNG(path, Rotate(landscape, d.Orientation)); err != nil {
		return err
	}
	full := forceFull || d.FullEvery <= 1 || d.shown%d.FullEvery == 0
	d.shown++
	if !d.OnKindle() {
		return WritePNG(filepath.Join(d.Dir, "screen-landscape.png"), landscape)
	}
	args := []string{"-q"}
	if full {
		args = append(args, "-f") // flashing refresh clears e-ink ghosting
	}
	args = append(args, "-g", "file="+path)
	if out, err := exec.Command(d.FBInk, args...).CombinedOutput(); err != nil {
		log.Printf("fbink failed: %v: %s", err, out)
		return err
	}
	return nil
}

// Rotate turns the landscape image so it reads upright when the Kindle stands on its side.
// usb-right: the Kindle is turned counter-clockwise, so the image is rotated clockwise.
func Rotate(src *image.Gray, orientation string) *image.Gray {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	dst := image.NewGray(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		row := src.Pix[y*src.Stride:]
		for x := 0; x < w; x++ {
			nx, ny := h-1-y, x
			if orientation == "usb-left" {
				nx, ny = y, w-1-x
			}
			dst.Pix[ny*dst.Stride+nx] = row[x]
		}
	}
	return dst
}

func WritePNG(path string, img image.Image) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, path)
}

func BatteryLevel() int {
	out, err := exec.Command("lipc-get-prop", "com.lab126.powerd", "battLevel").Output()
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return -1
	}
	return n
}

func WakeWifi() {
	exec.Command("lipc-set-prop", "com.lab126.cmd", "wirelessEnable", "1").Run()
}

// WifiIP returns the Kindle's address on wlan0 (or any non-loopback IPv4 elsewhere).
func WifiIP() string {
	ifaces, _ := net.Interfaces()
	best := ""
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			if ifc.Name == "wlan0" {
				return ipn.IP.String()
			}
			if best == "" {
				best = ipn.IP.String()
			}
		}
	}
	return best
}

// Firewall opens or closes a TCP port on wlan0 (the Kindle blocks inbound by default).
func Firewall(open bool, port int) {
	if _, err := exec.LookPath("iptables"); err != nil {
		return
	}
	op := "-D"
	if open {
		op = "-A"
	}
	exec.Command("iptables", op, "INPUT", "-i", "wlan0", "-p", "tcp", "--dport", strconv.Itoa(port), "-j", "ACCEPT").Run()
}
