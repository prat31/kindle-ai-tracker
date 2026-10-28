package main

import (
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"github.com/prat31/kindle-ai-tracker/internal/device"
	"github.com/prat31/kindle-ai-tracker/internal/history"
	"github.com/prat31/kindle-ai-tracker/internal/provider"
	"github.com/prat31/kindle-ai-tracker/internal/render"
)

// preview renders every screen with made-up data so layouts can be checked off-device.
func preview(cfg Config, out string) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	save := func(name string, c *render.Canvas) error {
		if err := device.WritePNG(filepath.Join(out, name+".png"), c.Img); err != nil {
			return err
		}
		return device.WritePNG(filepath.Join(out, name+"-device.png"), device.Rotate(c.Img, cfg.Orientation))
	}
	now := time.Now()
	hist, snap := fakeClaude(now, cfg.loc)
	v := &render.View{Now: now, Loc: cfg.loc, Provider: "Claude", Snap: snap, Hist: hist,
		NextUpdate: nextTick(now, cfg.RefreshMinutes), Battery: 82, Light: "66%", Pages: 1}

	v.Pages, v.Page = 3, 0 // shows the header arrows

	offline := *v
	offline.Err, offline.Hist = "No connection.", &history.Store{}

	paused := *v
	paused.Paused, paused.NextUpdate = true, time.Time{}

	asleep := *v
	asleep.Asleep = true

	lightOff := *v
	lightOff.Light = "Off"
	lightOff.Provider = "GitHub Copilot" // a long name, to check the header fits

	// A window that ended with nothing used since (this used to show "-97%").
	expired := *v
	es := *snap
	es.Meters = append([]provider.Meter{}, snap.Meters...)
	es.Meters[0].ResetsAt, es.Meters[0].Used = now.Add(-3*time.Hour), 22
	expired.Snap = &es

	copilot := *v
	copilot.Provider, copilot.Page = "GitHub Copilot", 1
	reset := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	copilot.Snap = &provider.Snapshot{Plan: "Individual", Fetched: now, Meters: []provider.Meter{{
		ID: "premium_interactions", Title: "Premium requests · monthly", Short: "Premium", Used: 76.3,
		Window: reset.Sub(reset.AddDate(0, -1, 0)), ResetsAt: reset, Detail: "71 of 300 left"}}}

	openrouter := *v
	openrouter.Provider, openrouter.Page = "OpenRouter", 2
	openrouter.Snap = &provider.Snapshot{Plan: "Paid", Fetched: now, Meters: []provider.Meter{
		{ID: "key_limit", Title: "Key credit limit · weekly", Short: "Key limit", Used: 25, Window: 7 * 24 * time.Hour,
			ResetsAt: now.Add(4 * 24 * time.Hour), Detail: "$2.50 of $10.00"},
		{ID: "credits", Title: "Account credits used", Short: "Credits", Used: 25, Detail: "$15.00 left of $20.00"}}}

	empty := &render.View{Now: now, Loc: cfg.loc, Provider: "AI Dashboard", Hist: &history.Store{}, Battery: 82}

	login := *v
	login.Snap, login.Pages = nil, 0
	prompt := provider.LoginPrompt{SignInURL: "https://claude.com/cai/oauth/authorize?code=true&client_id=9d1c250a-e61b-44d9-88ed-5944d1962f5e&response_type=code&redirect_uri=https%3A%2F%2Fplatform.claude.com%2Foauth%2Fcode%2Fcallback&scope=user%3Aprofile+user%3Ainference&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&state=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		SignInHint: "Approve access, then copy the code Claude shows you", InputLabel: "the code Claude showed you"}
	deviceLogin := login
	deviceLogin.Provider = "ChatGPT"
	devicePrompt := provider.LoginPrompt{SignInURL: "https://auth.openai.com/codex/device",
		SignInHint: "Sign in to ChatGPT and enter the code", UserCode: "K7QF-2MXP"}

	menu, _ := render.Menu(v, "Menu", []render.MenuItem{{Label: "Connectors", Sub: "3 connected · add or remove"},
		{Label: "Rotate 180°", Sub: "turn the screen upside down"}, {Label: "Refresh now"},
		{Label: "Exit dashboard", Sub: "back to the Kindle home screen"}, {Label: "Close menu"}}, "")
	connectors, _ := render.Menu(v, "Connectors", []render.MenuItem{
		{Label: "Claude", Sub: "connected · tap to disconnect"}, {Label: "ChatGPT", Sub: "tap to connect"},
		{Label: "GitHub Copilot", Sub: "connected · tap to disconnect"}, {Label: "OpenRouter", Sub: "connected · tap to disconnect"},
		{Label: "Z.ai GLM", Sub: "tap to connect"}, {Label: "Back"}}, "Tap Claude again to disconnect")
	bye := render.Message(&offline, "Dashboard stopped",
		"Restoring the Kindle home screen. This can take a minute or two. If it stays blank, press the power button twice.", "")

	for name, c := range map[string]*render.Canvas{
		"menu":                 menu,
		"connectors":           connectors,
		"exit":                 bye,
		"empty":                render.Message(empty, "No connectors yet", "Hold a finger on the screen for 3 seconds to open the menu, then choose Connectors to add Claude, ChatGPT, GitHub Copilot, OpenRouter or Z.ai."),
		"dashboard":            render.Dashboard(v),
		"dashboard-offline":    render.Dashboard(&offline),
		"dashboard-paused":     render.Dashboard(&paused),
		"dashboard-asleep":     render.Dashboard(&asleep),
		"dashboard-light-off":  render.Dashboard(&lightOff),
		"dashboard-expired":    render.Dashboard(&expired),
		"dashboard-copilot":    render.Dashboard(&copilot),
		"dashboard-openrouter": render.Dashboard(&openrouter),
		"login":                render.Login(&login, prompt, "http://192.168.1.42:8642", "ai-dashboard/data/claude/code.txt", "Waiting for the code Claude showed you…"),
		"login-device":         render.Login(&deviceLogin, devicePrompt, "", "", "Waiting for you to approve on auth.openai.com…"),
	} {
		if err := save(name, c); err != nil {
			return err
		}
	}
	var frames []*image.Gray
	for i, pv := range connectorPages(*v, now, cfg.loc) {
		c := render.Dashboard(&pv)
		if err := save(fmt.Sprintf("connector-%d", i+1), c); err != nil {
			return err
		}
		frames = append(frames, c.Img)
	}
	if err := device.WritePNG(filepath.Join(out, "cover.png"), render.Cover(300, 400)); err != nil {
		return err
	}
	if err := writeGIF(filepath.Join(out, "dashboards.gif"), frames, 250); err != nil {
		return err
	}
	fmt.Println("wrote previews to", out)
	return nil
}

// fakeUsage simulates 8 days of bursty use against a 5-hour and a weekly window and
// returns the history plus the current readings.
type fakeState struct {
	session, weekly       float64
	sessEnd, weeklyResets time.Time
}

func fakeUsage(now time.Time, loc *time.Location, seed uint64, weeklyRate float64) (*history.Store, fakeState) {
	rng := rand.New(rand.NewPCG(seed, 42))
	weekReset := now.Add(2*24*time.Hour + 5*time.Hour + 20*time.Minute).Truncate(time.Hour)
	wr := weekReset.Add(-7 * 24 * time.Hour)
	store := &history.Store{}
	var sessEnd time.Time
	s, w := 0.0, 0.0
	for t := now.Add(-8 * 24 * time.Hour); !t.After(now); t = t.Add(5 * time.Minute) {
		if !t.Before(wr) {
			wr, w = wr.Add(7*24*time.Hour), 0
		}
		if !t.Before(sessEnd) {
			s, sessEnd = 0, time.Time{}
		}
		h := t.In(loc).Hour()
		if busy := (h >= 9 && h < 12) || (h >= 14 && h < 18) || (h >= 21 && h < 23); busy && rng.Float64() < 0.55 {
			if sessEnd.IsZero() {
				sessEnd = t.Add(5 * time.Hour)
			}
			inc := rng.Float64() * 3.2
			s = math.Min(100, s+inc)
			w += inc * weeklyRate
		}
		snap := &provider.Snapshot{Fetched: t, Meters: []provider.Meter{
			{ID: "session", Used: s, ResetsAt: sessEnd}, {ID: "weekly", Used: w, ResetsAt: wr}}}
		store.Samples = append(store.Samples, history.FromSnapshot(snap))
	}
	if s < 20 { // make the preview's current session interesting
		s, sessEnd = 46, now.Add(2*time.Hour+47*time.Minute)
	}
	return store, fakeState{s, w, sessEnd, weekReset}
}

// remap renames meters in a fake history and scales them, to fake other providers.
func remap(src *history.Store, ids map[string]string, scale float64) *history.Store {
	out := &history.Store{}
	for _, smp := range src.Samples {
		m := map[string]history.Point{}
		for from, to := range ids {
			if p, ok := smp.M[from]; ok {
				m[to] = history.Point{U: math.Min(100, p.U*scale), R: p.R}
			}
		}
		out.Samples = append(out.Samples, history.Sample{T: smp.T, M: m})
	}
	return out
}

func fakeClaude(now time.Time, loc *time.Location) (*history.Store, *provider.Snapshot) {
	store, f := fakeUsage(now, loc, 7, 0.075)
	return store, &provider.Snapshot{Plan: "Pro", Fetched: now, Meters: []provider.Meter{
		{ID: "session", Title: "Current session · 5-hour limit", Short: "Session", Used: f.session, Window: 5 * time.Hour, ResetsAt: f.sessEnd},
		{ID: "weekly", Title: "Weekly limit · all models", Short: "Weekly", Used: f.weekly, Window: 7 * 24 * time.Hour, ResetsAt: f.weeklyResets},
		{ID: "weekly_sonnet", Title: "Weekly limit · Sonnet", Short: "Sonnet", Used: f.weekly * 0.4, Window: 7 * 24 * time.Hour, ResetsAt: f.weeklyResets},
	}}
}

// connectorPages builds one sample dashboard per connector, as they'd appear in turn.
func connectorPages(base render.View, now time.Time, loc *time.Location) []render.View {
	week := 7 * 24 * time.Hour
	claudeHist, claude := fakeClaude(now, loc)

	gptHist, g := fakeUsage(now, loc, 3, 0.11)
	gpt := &provider.Snapshot{Plan: "Plus", Fetched: now, Meters: []provider.Meter{
		{ID: "session", Title: "Current session · 5-hour limit", Short: "Session", Used: 63, Window: 5 * time.Hour, ResetsAt: now.Add(95 * time.Minute)},
		{ID: "weekly", Title: "Weekly limit · all models", Short: "Weekly", Used: g.weekly, Window: week, ResetsAt: g.weeklyResets}}}

	reset := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	copilot := &provider.Snapshot{Plan: "Pro", Fetched: now, Meters: []provider.Meter{{
		ID: "premium_interactions", Title: "Premium requests · monthly", Short: "Premium", Used: 76.3,
		Window: reset.Sub(reset.AddDate(0, -1, 0)), ResetsAt: reset, Detail: "71 of 300 left"}}}
	copilotHist := remap(claudeHist, map[string]string{"weekly": "premium_interactions"}, 2.1)

	_, wd := provider.NextPeriod("weekly", now)
	wreset, _ := provider.NextPeriod("weekly", now)
	openrouter := &provider.Snapshot{Plan: "Paid", Fetched: now, Meters: []provider.Meter{
		{ID: "key_limit", Title: "Key credit limit · weekly", Short: "Key limit", Used: 38, Window: wd, ResetsAt: wreset, Detail: "$3.80 of $10.00"},
		{ID: "credits", Title: "Account credits used", Short: "Credits", Used: 25, Detail: "$15.00 left of $20.00"}}}
	orHist := remap(gptHist, map[string]string{"weekly": "key_limit", "session": "credits"}, 0.9)

	zaiHist, z := fakeUsage(now, loc, 11, 0.05)
	zai := &provider.Snapshot{Plan: "Pro", Fetched: now, Meters: []provider.Meter{
		{ID: "session", Title: "Current session · 5-hour limit", Short: "Session", Used: z.session, Window: 5 * time.Hour, ResetsAt: z.sessEnd},
		{ID: "weekly", Title: "Weekly limit · tokens", Short: "Weekly", Used: z.weekly, Window: week, ResetsAt: z.weeklyResets},
		{ID: "mcp", Title: "MCP tool calls · monthly", Short: "MCP", Used: 12, Window: 30 * 24 * time.Hour, ResetsAt: now.Add(19 * 24 * time.Hour)}}}

	pages := []struct {
		name string
		snap *provider.Snapshot
		hist *history.Store
	}{{"Claude", claude, claudeHist}, {"ChatGPT", gpt, gptHist}, {"GitHub Copilot", copilot, copilotHist},
		{"OpenRouter", openrouter, orHist}, {"Z.ai GLM", zai, zaiHist}}
	views := make([]render.View, len(pages))
	for i, pg := range pages {
		v := base
		v.Provider, v.Snap, v.Hist, v.Page, v.Pages = pg.name, pg.snap, pg.hist, i, len(pages)
		views[i] = v
	}
	return views
}

// writeGIF scales each landscape frame down by half, sets it in a drawing of a
// Paperwhite lying on its side (USB-C edge on the right, so the wider bezel is there)
// and loops through them. Outside the Kindle is transparent.
func writeGIF(path string, frames []*image.Gray, delay int) error {
	pal := make(color.Palette, 17)
	for i := 0; i < 16; i++ {
		pal[i] = color.Gray{Y: uint8(i * 17)}
	}
	const clear = 16
	pal[clear] = color.Transparent
	anim := &gif.GIF{}

	const (
		bezel   = 38 // left, top and bottom
		chin    = 88 // the wider bezel on the USB side
		radius  = 46 // corners of the body
		bodyIdx = 2  // near-black body
		edgeIdx = 4  // slightly lighter rim around the screen
		portIdx = 0  // USB-C port
		portLen = 44 // port length along the edge
		portW   = 6
	)
	for _, f := range frames {
		sw, sh := f.Rect.Dx()/2, f.Rect.Dy()/2
		w, h := sw+bezel+chin, sh+2*bezel
		img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.Pix[y*img.Stride+x] = bodyIdx
				if outsideRounded(x, y, w, h, radius) {
					img.Pix[y*img.Stride+x] = clear
				}
			}
		}
		// a thin rim where the screen sits in the bezel
		for y := bezel - 3; y < bezel+sh+3; y++ {
			for x := bezel - 3; x < bezel+sw+3; x++ {
				img.Pix[y*img.Stride+x] = edgeIdx
			}
		}
		for y := 0; y < sh; y++ {
			for x := 0; x < sw; x++ {
				sum := int(f.Pix[2*y*f.Stride+2*x]) + int(f.Pix[2*y*f.Stride+2*x+1]) +
					int(f.Pix[(2*y+1)*f.Stride+2*x]) + int(f.Pix[(2*y+1)*f.Stride+2*x+1])
				img.Pix[(y+bezel)*img.Stride+x+bezel] = uint8((sum/4 + 8) / 17)
			}
		}
		// "kindle" along the wide bezel, reading bottom to top since the device lies on its side
		logo := render.NewCanvas(200, 50)
		logo.Rect(0, 0, 200, 50, 0x22)
		logo.Text("kindle", render.Face(render.Medium, 34), 100, 36, render.Center, 0x99)
		rot := device.Rotate(logo.Img, "usb-left")
		ox, oy := bezel+sw+(chin-rot.Rect.Dx())/2, (h-rot.Rect.Dy())/2
		for y := 0; y < rot.Rect.Dy(); y++ {
			for x := 0; x < rot.Rect.Dx(); x++ {
				img.Pix[(oy+y)*img.Stride+ox+x] = uint8((int(rot.Pix[y*rot.Stride+x]) + 8) / 17)
			}
		}
		// USB-C port on the right edge
		for y := h/2 - portLen/2; y < h/2+portLen/2; y++ {
			for x := w - portW; x < w-1; x++ {
				img.Pix[y*img.Stride+x] = portIdx
			}
		}
		anim.Image = append(anim.Image, img)
		anim.Delay = append(anim.Delay, delay)
		anim.Disposal = append(anim.Disposal, gif.DisposalBackground)
	}
	anim.BackgroundIndex = clear
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	return gif.EncodeAll(out, anim)
}

// outsideRounded reports whether (x, y) falls outside a w×h rectangle with corners of radius r.
func outsideRounded(x, y, w, h, r int) bool {
	cx, cy := -1, -1
	switch {
	case x < r && y < r:
		cx, cy = r, r
	case x >= w-r && y < r:
		cx, cy = w-r-1, r
	case x < r && y >= h-r:
		cx, cy = r, h-r-1
	case x >= w-r && y >= h-r:
		cx, cy = w-r-1, h-r-1
	default:
		return false
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy > r*r
}
