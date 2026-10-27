// ai-dash turns a jailbroken Kindle into a desk display for AI plan usage.
// Each provider (see internal/provider) can be connected from the Kindle's menu; it then
// logs in from the Kindle itself, is polled on a timer, and its meters are drawn with
// FBInk. History for trends is kept per provider.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "golang.org/x/crypto/x509roots/fallback" // CA roots, in case the Kindle's store is old
	_ "time/tzdata"

	"github.com/prat31/kindle-ai-tracker/internal/device"
	"github.com/prat31/kindle-ai-tracker/internal/history"
	"github.com/prat31/kindle-ai-tracker/internal/provider"
	_ "github.com/prat31/kindle-ai-tracker/internal/provider/claude"
	_ "github.com/prat31/kindle-ai-tracker/internal/provider/codex"
	_ "github.com/prat31/kindle-ai-tracker/internal/provider/copilot"
	_ "github.com/prat31/kindle-ai-tracker/internal/provider/openrouter"
	_ "github.com/prat31/kindle-ai-tracker/internal/provider/zai"
	"github.com/prat31/kindle-ai-tracker/internal/render"
)

// source is one supported provider and what we know about it.
type source struct {
	p    provider.Provider
	dir  string
	hist *history.Store
	snap *provider.Snapshot // last successful reading
	err  string             // latest failure, cleared on success
}

type App struct {
	cfg       Config
	dir       string
	display   *device.Display
	touch     *device.Touch
	all       []*source // every supported provider, in menu order
	connected []*source // the ones shown, in the order they were connected
	page      int
	next      time.Time // zero while paused
	idle      int       // fetch rounds in a row that brought nothing new
	paused    bool
}

func main() {
	exe, _ := os.Executable()
	dir := flag.String("dir", filepath.Dir(exe), "data directory (config.json, state.json, data/<provider>/...)")
	out := flag.String("out", ".", "preview: output directory")
	flag.Parse()
	cfg := loadConfig(*dir)

	var err error
	switch cmd := flag.Arg(0); cmd {
	case "preview":
		err = preview(cfg, *out)
	case "providers":
		fmt.Println(strings.Join(provider.IDs(), "\n"))
	case "", "run", "once", "login-cli":
		var a *App
		if a, err = newApp(cfg, *dir); err == nil {
			switch cmd {
			case "once":
				err = a.once()
			case "login-cli":
				err = a.loginCLI(flag.Arg(1))
			default:
				err = a.run()
			}
		}
	default:
		err = fmt.Errorf("unknown command %q (run, once, login-cli <provider>, preview, providers)", cmd)
	}
	if err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func newApp(cfg Config, dir string) (*App, error) {
	a := &App{cfg: cfg, dir: dir, touch: device.NoTouch(),
		display: &device.Display{Orientation: cfg.Orientation, FBInk: cfg.FBInk, Dir: dir, FullEvery: cfg.FullRefreshEvery}}
	st := loadState(dir)
	if st.Orientation != "" {
		a.display.Orientation = st.Orientation // set from the on-device menu
	}
	for _, id := range provider.IDs() {
		pdir := filepath.Join(dir, "data", id)
		if err := os.MkdirAll(pdir, 0o755); err != nil {
			return nil, err
		}
		p, err := provider.New(id, pdir, cfg.optionsFor(id))
		if err != nil {
			return nil, err
		}
		a.all = append(a.all, &source{p: p, dir: pdir, hist: history.Open(filepath.Join(pdir, "history.jsonl"))})
	}
	if st.Connectors == nil {
		// state.json from before connectors existed: keep whatever is already logged in.
		for _, src := range a.all {
			if src.p.LoggedIn() {
				a.connected = append(a.connected, src)
			}
		}
		a.saveState()
	} else {
		for _, id := range *st.Connectors {
			if src := a.byID(id); src != nil {
				a.connected = append(a.connected, src)
			}
		}
	}
	return a, nil
}

func (a *App) byID(id string) *source {
	for _, src := range a.all {
		if src.p.ID() == id {
			return src
		}
	}
	return nil
}

func (a *App) isConnected(src *source) bool {
	for _, c := range a.connected {
		if c == src {
			return true
		}
	}
	return false
}

func (a *App) saveState() {
	ids := []string{}
	for _, src := range a.connected {
		ids = append(ids, src.p.ID())
	}
	saveState(a.dir, State{Orientation: a.display.Orientation, Connectors: &ids})
}

func (a *App) current() *source {
	if len(a.connected) == 0 {
		return nil
	}
	a.page = (a.page%len(a.connected) + len(a.connected)) % len(a.connected)
	return a.connected[a.page]
}

func (a *App) view(src *source) *render.View {
	v := &render.View{Now: time.Now(), Loc: a.cfg.loc, Provider: "AI Dashboard", NextUpdate: a.next,
		Battery: device.BatteryLevel(), Page: a.page, Pages: len(a.connected), Paused: a.paused, Hist: &history.Store{}}
	if src != nil {
		v.Provider, v.Snap, v.Err, v.Hist = src.p.Name(), src.snap, src.err, src.hist
	}
	return v
}

func (a *App) screen(src *source) *render.Canvas {
	v := a.view(src)
	switch {
	case src == nil:
		return render.Message(v, "No connectors yet",
			"Hold a finger on the screen for 3 seconds to open the menu, then choose Connectors to add Claude, ChatGPT, GitHub Copilot, OpenRouter or Z.ai.")
	case !src.p.LoggedIn():
		return render.Message(v, src.p.Name()+" needs you to log in again",
			"Hold 3 seconds for the menu, then Connectors → "+src.p.Name()+" to reconnect.")
	case src.snap != nil:
		return render.Dashboard(v)
	case src.err != "":
		return render.Message(v, "Can't reach "+src.p.Name()+" right now", src.err+" — retrying at "+v.Clock(a.next))
	default:
		return render.Message(v, "Fetching your usage…", "")
	}
}

func (a *App) showPage(full bool) {
	a.display.Show(a.screen(a.current()).Img, full)
}

func (a *App) run() error {
	a.touch = device.WatchTouch(a.cfg.Calibration, render.H, render.W) // portrait panel
	a.display.Show(render.Message(a.view(nil), "Starting AI dashboard…",
		"Hold a finger on the screen for 3 seconds to open the menu.").Img, true)
	for {
		if len(a.connected) > 0 && !a.paused {
			a.fetchAll()
		}
		a.showPage(false)

		drainGestures(a.touch.Tap) // taps while we were busy don't count
		// Poll the wall clock: monotonic timers stall while the Kindle is suspended.
		tick := time.NewTicker(15 * time.Second)
	wait:
		for {
			select {
			case <-tick.C:
				if len(a.connected) > 0 && !a.paused && !time.Now().Before(a.next) {
					break wait
				}
			case g := <-a.touch.Tap:
				switch a.gesture(g) {
				case navPrev:
					a.page--
					a.showPage(false)
				case navNext:
					a.page++
					a.showPage(false)
				case navTap:
					a.resume()
					break wait // refresh now
				}
			case <-a.touch.Hold:
				switch a.mainMenu() {
				case actExit:
					tick.Stop()
					return a.bye()
				case actRefresh, actChanged:
					a.resume()
					break wait
				}
				a.showPage(true)
			}
		}
		tick.Stop()
	}
}

type nav int

const (
	navTap nav = iota
	navPrev
	navNext
)

// gesture classifies a touch on the dashboard: a horizontal swipe or a tap on the header
// arrows moves between connectors (cyclically), any other tap means "refresh".
func (a *App) gesture(g device.Gesture) nav {
	if g.End.X < 0 {
		return navTap
	}
	sx, sy := device.ToLandscape(g.Start, a.display.Orientation, render.W, render.H)
	ex, ey := device.ToLandscape(g.End, a.display.Orientation, render.W, render.H)
	dx, dy := float64(ex-sx), float64(ey-sy)
	if len(a.connected) < 2 {
		return navTap
	}
	if math.Abs(dx) >= 160 && math.Abs(dx) > 1.5*math.Abs(dy) {
		if dx < 0 {
			return navNext // finger moved left: bring in the next page from the right
		}
		return navPrev
	}
	p := image.Pt(ex, ey)
	switch {
	case p.In(render.NavPrev.Inset(-24)):
		return navPrev
	case p.In(render.NavNext.Inset(-24)):
		return navNext
	}
	return navTap
}

// fetchAll refreshes every connected provider and tracks whether anything changed.
func (a *App) fetchAll() {
	a.next = nextTick(time.Now(), a.cfg.RefreshMinutes)
	changed := false
	for _, src := range a.connected {
		if !src.p.LoggedIn() {
			continue
		}
		c, err := a.update(src)
		if errors.Is(err, provider.ErrReauth) {
			log.Printf("%s: login no longer valid: %v", src.p.ID(), err)
			src.p.Logout()
		}
		changed = changed || c
	}
	a.trackIdle(changed)
}

// trackIdle pauses auto-refresh after cfg.PauseAfter rounds in a row in which no
// provider reported anything new (every fetch failed or returned the same numbers).
func (a *App) trackIdle(changed bool) {
	if changed {
		a.idle = 0
		return
	}
	a.idle++
	if a.cfg.PauseAfter > 0 && a.idle >= a.cfg.PauseAfter {
		a.paused, a.next = true, time.Time{}
		log.Printf("no new data in %d fetches; auto-refresh paused until the screen is tapped", a.idle)
	}
}

func (a *App) resume() {
	if a.paused {
		log.Printf("auto-refresh resumed")
	}
	a.paused, a.idle = false, 0
}

func (a *App) bye() error {
	a.display.Show(render.Message(a.view(a.current()), "Dashboard stopped",
		"Restoring the Kindle home screen. This can take a minute or two. If it stays blank, press the power button twice.", "").Img, true)
	return nil
}

func nextTick(now time.Time, minutes int) time.Time {
	step := time.Duration(minutes) * time.Minute
	return now.Truncate(step).Add(step)
}

// update fetches one provider and reports whether its numbers changed. Only ErrReauth
// is returned; other failures are kept in src.err and shown on screen.
func (a *App) update(src *source) (changed bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	snap, raw, err := src.p.Fetch(ctx)
	if raw != nil {
		os.WriteFile(filepath.Join(src.dir, "last_response.json"), raw, 0o644)
	}
	if errors.Is(err, provider.ErrReauth) {
		return false, err
	}
	if err != nil {
		log.Printf("%s: update failed: %v", src.p.ID(), err)
		var ne net.Error
		if errors.As(err, &ne) {
			src.err = "No connection."
			device.WakeWifi()
		} else {
			src.err = err.Error()
		}
		return false, nil
	}
	changed = src.snap == nil || meterKey(src.snap) != meterKey(snap)
	src.snap, src.err = snap, ""
	src.hist.Add(history.FromSnapshot(snap))
	var parts []string
	for _, m := range snap.Meters {
		parts = append(parts, fmt.Sprintf("%s=%.0f%%", m.ID, m.Used))
	}
	log.Printf("%s: %s (changed=%v)", src.p.ID(), strings.Join(parts, " "), changed)
	return changed, nil
}

// meterKey summarises the numbers a snapshot shows, for change detection.
func meterKey(s *provider.Snapshot) string {
	var b strings.Builder
	for _, m := range s.Meters {
		fmt.Fprintf(&b, "%s=%.1f;", m.ID, m.Used)
	}
	return b.String()
}

func (a *App) once() error {
	if len(a.connected) == 0 {
		return errors.New("no connectors; run login-cli <provider> first")
	}
	a.next = nextTick(time.Now(), a.cfg.RefreshMinutes)
	for _, src := range a.connected {
		if _, err := a.update(src); err != nil {
			return err
		}
	}
	a.showPage(true)
	return nil
}

// loginCLI connects a provider from a terminal, for testing off the Kindle.
func (a *App) loginCLI(id string) error {
	src := a.byID(id)
	if src == nil {
		return fmt.Errorf("unknown provider %q (available: %v)", id, provider.IDs())
	}
	ctx := context.Background()
	prompt, err := src.p.BeginLogin(ctx)
	if err != nil {
		return err
	}
	if prompt.SignInURL != "" {
		fmt.Printf("Open %s\n%s.\n", prompt.SignInURL, prompt.SignInHint)
	}
	if prompt.UserCode != "" {
		fmt.Printf("Code: %s\nWaiting for approval…\n", prompt.UserCode)
		poller, _ := src.p.(provider.Poller)
		for {
			time.Sleep(prompt.PollEvery)
			done, err := poller.PollLogin(ctx)
			if err != nil {
				return err
			}
			if done {
				break
			}
		}
	} else {
		fmt.Printf("Paste %s: ", prompt.InputLabel)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if err := src.p.CompleteLogin(ctx, line); err != nil {
			return err
		}
	}
	if !a.isConnected(src) {
		a.connected = append(a.connected, src)
		a.saveState()
	}
	fmt.Println("Connected", src.p.Name())
	return nil
}
