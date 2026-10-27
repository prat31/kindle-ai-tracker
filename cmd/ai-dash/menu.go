package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/prat31/kindle-ai-tracker/internal/device"
	"github.com/prat31/kindle-ai-tracker/internal/render"
)

// State is what the on-device menu changes. It lives in state.json so config.json,
// which people edit by hand, is never rewritten.
type State struct {
	Orientation string `json:"orientation,omitempty"`
	// Connectors shown on the dashboard, in order. nil means state.json predates
	// connectors (see newApp); an empty list means none.
	Connectors *[]string `json:"connectors,omitempty"`
}

func statePath(dir string) string { return filepath.Join(dir, "state.json") }

func loadState(dir string) State {
	var s State
	if b, err := os.ReadFile(statePath(dir)); err == nil {
		json.Unmarshal(b, &s)
	}
	return s
}

func saveState(dir string, s State) {
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(statePath(dir), b, 0o644); err != nil {
		log.Printf("saving state: %v", err)
	}
}

type menuAction int

const (
	actClose   menuAction = iota
	actRefresh            // refresh now
	actExit               // stop the dashboard
	actChanged            // connectors were added or removed
)

const menuTimeout = 20 * time.Second

// pick shows a menu and waits for a button to be tapped. It returns the button's index,
// or -1 if nothing was tapped before the timeout.
func (a *App) pick(title string, items []render.MenuItem, status string) int {
	for {
		c, rects := render.Menu(a.view(a.current()), title, items, status)
		a.display.Show(c.Img, true)
		drainGestures(a.touch.Tap)
		timeout := time.NewTimer(menuTimeout)
		var g device.Gesture
		select {
		case g = <-a.touch.Tap:
			timeout.Stop()
		case <-a.touch.Hold:
			timeout.Stop()
			continue
		case <-timeout.C:
			return -1
		}
		if g.End.X < 0 {
			status = "Couldn't read where you tapped"
			continue
		}
		x, y := device.ToLandscape(g.End, a.display.Orientation, render.W, render.H)
		for i, r := range rects {
			if image.Pt(x, y).In(r) {
				log.Printf("menu %q: tapped %q", title, items[i].Label)
				return i
			}
		}
		status = "Tap one of the buttons"
	}
}

func (a *App) mainMenu() menuAction {
	status := ""
	for {
		n := len(a.connected)
		items := []render.MenuItem{
			{Label: "Connectors", Sub: fmt.Sprintf("%d connected · add or remove", n)},
			{Label: "Rotate 180°", Sub: "turn the screen upside down"},
			{Label: "Refresh now"},
			{Label: "Exit dashboard", Sub: "back to the Kindle home screen"},
			{Label: "Close menu"},
		}
		switch a.pick("Menu", items, status) {
		case 0:
			if a.connectorsMenu() {
				return actChanged
			}
			status = ""
		case 1:
			a.rotate180()
			status = "Rotated"
		case 2:
			return actRefresh
		case 3:
			return actExit
		default:
			return actClose
		}
	}
}

// connectorsMenu lists every supported provider. Tapping one that isn't connected
// starts its login; tapping a connected one twice disconnects it. It reports whether
// anything changed.
func (a *App) connectorsMenu() bool {
	changed, armed, status := false, "", ""
	for {
		items := make([]render.MenuItem, 0, len(a.all)+1)
		for _, src := range a.all {
			sub := "tap to connect"
			switch {
			case a.isConnected(src) && src.p.LoggedIn():
				sub = "connected · tap to disconnect"
			case a.isConnected(src):
				sub = "login expired · tap to reconnect"
			}
			items = append(items, render.MenuItem{Label: src.p.Name(), Sub: sub})
		}
		items = append(items, render.MenuItem{Label: "Back"})

		i := a.pick("Connectors", items, status)
		if i < 0 || i == len(a.all) {
			return changed
		}
		src := a.all[i]
		if a.isConnected(src) && src.p.LoggedIn() {
			if armed != src.p.ID() {
				armed, status = src.p.ID(), "Tap "+src.p.Name()+" again to disconnect"
				continue
			}
			a.disconnect(src)
			armed, status, changed = "", "Disconnected "+src.p.Name(), true
			continue
		}
		armed = ""
		switch err := a.login(src); {
		case err == nil:
			if !a.isConnected(src) {
				a.connected = append(a.connected, src)
			}
			a.saveState()
			src.err = ""
			for j, c := range a.connected {
				if c == src {
					a.page = j // show the new connector straight away
				}
			}
			return true
		case errors.Is(err, errCancel):
			status = "Cancelled"
		default:
			status = "Couldn't connect " + src.p.Name() + ": " + err.Error()
		}
	}
}

func (a *App) disconnect(src *source) {
	src.p.Logout()
	src.snap, src.err = nil, ""
	kept := a.connected[:0]
	for _, c := range a.connected {
		if c != src {
			kept = append(kept, c)
		}
	}
	a.connected = kept
	a.saveState()
	log.Printf("%s disconnected", src.p.ID())
}

// loginMenu is the menu while a login screen is up.
func (a *App) loginMenu() (cancel bool) {
	status := ""
	for {
		items := []render.MenuItem{
			{Label: "Cancel connecting"},
			{Label: "Rotate 180°", Sub: "turn the screen upside down"},
			{Label: "Close menu"},
		}
		switch a.pick("Menu", items, status) {
		case 0:
			return true
		case 1:
			a.rotate180()
			status = "Rotated"
		default:
			return false
		}
	}
}

func (a *App) rotate180() {
	if a.display.Orientation == "usb-left" {
		a.display.Orientation = "usb-right"
	} else {
		a.display.Orientation = "usb-left"
	}
	a.saveState()
}

func drainGestures(ch chan device.Gesture) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
