package render

import (
	"fmt"
	"image"
	"math"
	"net/url"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"golang.org/x/image/font"

	"github.com/prat31/kindle-ai-tracker/internal/history"
	"github.com/prat31/kindle-ai-tracker/internal/provider"
)

// Landscape canvas; the device package rotates it onto the portrait framebuffer.
const (
	W      = 1648
	H      = 1236
	margin = 56
)

type View struct {
	Now        time.Time
	Loc        *time.Location
	Provider   string             // header name, e.g. "Claude"
	Snap       *provider.Snapshot // last successful reading, nil if none yet
	Err        string             // set when the latest refresh failed
	Hist       *history.Store
	NextUpdate time.Time
	Battery    int // -1 if unknown
	Page       int // 0-based, when several providers take turns on screen
	Pages      int
	Paused     bool // auto-refresh stopped after a run of unchanged fetches
}

func (v *View) Clock(t time.Time) string { return t.In(v.Loc).Format("15:04") }

func (v *View) dayClock(t time.Time) string {
	lt := t.In(v.Loc)
	if history.DayStart(lt).Equal(history.DayStart(v.Now.In(v.Loc))) {
		return lt.Format("15:04")
	}
	return lt.Format("Mon 15:04")
}

func caps(s string) string { return strings.ReplaceAll(strings.ToUpper(s), " · ", "  ·  ") }

// Dashboard lays out any provider's meters: the first two get big panels, the rest are
// listed underneath, and the charts follow the shortest and the longest limit windows.
func Dashboard(v *View) *Canvas {
	c := NewCanvas(W, H)
	drawHeader(c, v)
	meters := v.Snap.Meters
	colW := (W - 3*margin) / 2
	cols := []int{margin, margin*2 + colW}

	big := meters[:min(2, len(meters))]
	var extras []string
	for _, m := range meters[len(big):] {
		extras = append(extras, fmt.Sprintf("%s %.0f%%", m.Short, m.Used))
	}
	if len(big) == 1 {
		drawMeterPanel(c, v, margin, 170, W-2*margin, big[0], "")
	}
	for i, m := range big {
		if len(big) == 1 {
			break
		}
		extra := ""
		if i == len(big)-1 {
			extra = strings.Join(extras, "   ·   ")
		}
		drawMeterPanel(c, v, cols[i], 170, colW, m, extra)
	}

	c.Rect(margin, 680, W-2*margin, 2, cLight)
	drawLineChart(c, v, cols[0], 712, colW, 430, big[0])
	if long := perDayMeter(big, meters); long != nil {
		drawPerDayBars(c, v, cols[1], 712, colW, 430, *long)
	} else {
		drawPeakBars(c, v, cols[1], 712, colW, 430, big[0])
	}
	hint := "Tap: refresh   ·   Hold 3 s: menu   ·   Bar marker = time elapsed"
	switch {
	case v.Paused:
		hint = "Tap: refresh and resume   ·   Hold 3 s: menu"
	case v.Pages > 1:
		hint = "Tap: refresh   ·   Swipe or arrows: switch   ·   Hold 3 s: menu"
	}
	drawFooter(c, v, hint)
	return c
}

// perDayMeter picks the limit for the per-day chart: a multi-day limit shown in a big
// panel if there is one (e.g. weekly), otherwise the longest multi-day limit.
func perDayMeter(big, all []provider.Meter) *provider.Meter {
	for i := len(big) - 1; i >= 0; i-- {
		if big[i].Window >= 48*time.Hour {
			return &big[i]
		}
	}
	var best *provider.Meter
	for i := range all {
		if all[i].Window >= 48*time.Hour && (best == nil || all[i].Window > best.Window) {
			best = &all[i]
		}
	}
	return best
}

// Header navigation buttons, shown when several connectors take turns on screen.
var (
	NavPrev = image.Rect(W-margin-2*96-20, 40, W-margin-96-20, 116)
	NavNext = image.Rect(W-margin-96, 40, W-margin, 116)
)

func drawHeader(c *Canvas, v *View) {
	w := c.Text(v.Provider, Face(Bold, 60), margin, 100, Left, cBlack)
	sub := "usage"
	if v.Snap != nil && v.Snap.Plan != "" {
		sub = v.Snap.Plan + " usage"
	}
	c.Text("  "+sub, Face(Regular, 40), margin+w, 100, Left, cDark)
	right := W - margin
	if v.Pages > 1 {
		drawNavButton(c, NavPrev, false)
		drawNavButton(c, NavNext, true)
		right = NavPrev.Min.X - 28
	}
	chip := func(label string) {
		f := Face(Bold, 32)
		tw := TextWidth(f, label)
		c.RoundRect(right-tw-40, 46, tw+40, 64, 14, cBlack)
		c.Text(label, f, right-20, 90, Right, cWhite)
	}
	switch {
	case v.Err != "":
		last := "never"
		if v.Snap != nil {
			last = v.Clock(v.Snap.Fetched)
		}
		chip("OFFLINE  ·  last data " + last)
	case v.Snap != nil && v.Paused:
		chip("PAUSED  ·  last update " + v.Clock(v.Snap.Fetched))
	case v.Snap != nil:
		c.Text("Updated "+v.Clock(v.Snap.Fetched), Face(Medium, 36), right, 92, Right, cBlack)
	}
	c.Rect(margin, 130, W-2*margin, 4, cBlack)
}

// drawNavButton draws a rounded button with a solid triangle pointing left or right.
func drawNavButton(c *Canvas, r image.Rectangle, right bool) {
	c.RoundRect(r.Min.X, r.Min.Y, r.Dx(), r.Dy(), 16, cBlack)
	c.RoundRect(r.Min.X+4, r.Min.Y+4, r.Dx()-8, r.Dy()-8, 12, cWhite)
	cx, cy, half := r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2, 18
	for dx := -half; dx <= half; dx++ {
		// height shrinks towards the point of the triangle
		h := half - dx // pointing right: tall on the left, a point on the right
		if !right {
			h = half + dx
		}
		c.Rect(cx+dx, cy-h/2, 1, h+1, cBlack)
	}
}

func drawFooter(c *Canvas, v *View, hint string) {
	c.Text(hint, Face(Regular, 27), margin, H-40, Left, cMid)
	var right []string
	if v.Pages > 1 {
		right = append(right, fmt.Sprintf("Page %d/%d", v.Page+1, v.Pages))
	}
	if v.Paused {
		right = append(right, "No changes lately: auto-refresh paused")
	} else if !v.NextUpdate.IsZero() {
		right = append(right, "Next update "+v.Clock(v.NextUpdate))
	}
	if v.Battery >= 0 {
		right = append(right, fmt.Sprintf("Battery %d%%", v.Battery))
	}
	c.Text(strings.Join(right, "   ·   "), Face(Medium, 27), W-margin, H-40, Right, cDark)
}

func drawMeterPanel(c *Canvas, v *View, x, top, w int, m provider.Meter, extra string) {
	c.Text(caps(m.Title), Face(Medium, 30), x, top+30, Left, cMid)
	// A reset time in the past means the window ended and nothing has been used since;
	// the service keeps reporting the old window until the next message.
	expired := !m.ResetsAt.IsZero() && !m.ResetsAt.After(v.Now)
	used := m.Used
	if expired {
		used = 0
	}
	c.Text(fmt.Sprintf("%.0f%%", used), Face(Bold, 170), x-6, top+205, Left, cBlack)

	switch {
	case expired:
		c.Text("window", Face(Regular, 32), x+w, top+90, Right, cDark)
		c.Text("reset", Face(Bold, 60), x+w, top+158, Right, cBlack)
		c.Text("at "+v.dayClock(m.ResetsAt), Face(Regular, 32), x+w, top+202, Right, cDark)
	case !m.ResetsAt.IsZero():
		c.Text("resets in", Face(Regular, 32), x+w, top+90, Right, cDark)
		c.Text(fmtDuration(m.ResetsAt.Sub(v.Now)), Face(Bold, 60), x+w, top+158, Right, cBlack)
		c.Text("at "+v.dayClock(m.ResetsAt), Face(Regular, 32), x+w, top+202, Right, cDark)
	case m.Detail != "":
		c.Text(m.Detail, FitFace(Bold, 52, m.Detail, w/2), x+w, top+158, Right, cBlack)
	}

	// Usage bar, with a marker for how much of the window has elapsed.
	by, bh := top+240, 50
	c.RoundRect(x, by, w, bh, bh/2, cBlack)
	c.RoundRect(x+4, by+4, w-8, bh-8, (bh-8)/2, cWhite)
	if used > 0 {
		fw := int(math.Round(float64(w) * math.Min(used, 100) / 100))
		c.RoundRect(x, by, max(fw, bh), bh, bh/2, cBlack)
	}
	if !expired && !m.ResetsAt.IsZero() && m.Window > 0 {
		frac := 1 - m.ResetsAt.Sub(v.Now).Seconds()/m.Window.Seconds()
		mx := x + int(math.Max(0, math.Min(1, frac))*float64(w))
		c.Rect(mx-7, by-16, 14, bh+32, cWhite)
		c.Rect(mx-3, by-16, 6, bh+32, cBlack)
	}

	var l1, l2 string
	if expired {
		l1 = "Fresh window: starts with your next message"
		l2 = fmt.Sprintf("Last window ended at %.0f%%", m.Used)
	} else {
		l1, l2 = paceLines(v, m)
	}
	if m.Detail != "" && !m.ResetsAt.IsZero() {
		l2 = joinDot(m.Detail, l2) // otherwise it's already in the top right
	}
	c.Text(l1, FitFace(Bold, 36, l1, w), x, top+352, Left, cBlack)
	c.Text(l2, FitFace(Regular, 32, l2, w), x, top+402, Left, cDark)
	if extra != "" {
		c.Text(extra, FitFace(Regular, 32, extra, w), x, top+452, Left, cDark)
	}
}

func joinDot(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, "   ·   ")
}

// paceLines: short windows (a day or less) are judged by recent burn rate,
// long windows by how far ahead of an even spread across the window you are.
// Callers handle windows whose reset time has already passed.
func paceLines(v *View, m provider.Meter) (string, string) {
	switch {
	case m.Used >= 100:
		return "Limit reached", "Resets " + v.dayClock(m.ResetsAt)
	case m.Window <= 0 || m.ResetsAt.IsZero():
		return "", ""
	case m.Window <= 24*time.Hour:
		return burnLines(v, m)
	default:
		return evenPaceLines(v, m)
	}
}

func burnLines(v *View, m provider.Meter) (string, string) {
	left := m.ResetsAt.Sub(v.Now)
	peak := m.Used
	for _, d := range v.Hist.PeakPerDay(m.ID, v.Now, v.Loc, 1) {
		peak = math.Max(peak, d.Value)
	}
	// The fastest you can go from here and still not hit the limit before the reset.
	safe := fmt.Sprintf("safe up to %.0f%%/h", (100-m.Used)/math.Max(left.Hours(), 0.1))
	stats := joinDot(safe, fmt.Sprintf("peak today %.0f%%", peak))
	rate, ok := v.Hist.Rate(m, v.Now)
	switch {
	case !ok:
		return "Measuring pace…", stats
	case rate < 0.5:
		return fmt.Sprintf("Idle for the last hour: %.0f%% left", 100-m.Used), stats
	}
	stats = joinDot(fmt.Sprintf("Burn %.0f%%/h", rate), stats)
	toLimit := time.Duration((100 - m.Used) / rate * float64(time.Hour))
	if toLimit < left {
		return fmt.Sprintf("At this pace: limit at %s (in %s)", v.Clock(v.Now.Add(toLimit)), fmtDuration(toLimit)), stats
	}
	proj := math.Min(100, m.Used+rate*left.Hours())
	return fmt.Sprintf("On track: ~%.0f%% used by the %s reset", proj, v.Clock(m.ResetsAt)), stats
}

func evenPaceLines(v *View, m provider.Meter) (string, string) {
	elapsed := m.Window - m.ResetsAt.Sub(v.Now)
	frac := elapsed.Seconds() / m.Window.Seconds()
	if frac < 0.02 {
		return "New window just started", ""
	}
	var l1 string
	switch diff := m.Used - frac*100; {
	case diff > 3:
		l1 = fmt.Sprintf("%.0f pts ahead of even pace", diff)
	case diff < -3:
		l1 = fmt.Sprintf("%.0f pts under even pace", -diff)
	default:
		l1 = "Right on even pace"
	}
	l1 += fmt.Sprintf(" (%.0f%% of window gone)", frac*100)
	if proj := m.Used / frac; proj <= 100 {
		return l1, fmt.Sprintf("Projected ~%.0f%% by reset", proj)
	}
	perHour := m.Used / elapsed.Hours()
	hit := v.Now.Add(time.Duration((100 - m.Used) / perHour * float64(time.Hour)))
	return l1, "At this pace: limit around " + v.dayClock(hit)
}

func drawChartTitle(c *Canvas, x, top, w int, title, sub string) {
	c.Text(caps(title), Face(Medium, 30), x, top+30, Left, cMid)
	if sub != "" {
		c.Text(sub, Face(Regular, 28), x+w, top+30, Right, cDark)
	}
}

func drawEmpty(c *Canvas, x0, y0, x1, y1 int) {
	c.Rect(x0, y1, x1-x0, 3, cBlack)
	cx, cy := (x0+x1)/2, (y0+y1)/2
	c.Text("Collecting data…", Face(Medium, 36), cx, cy, Center, cMid)
	c.Text("Trends appear after a few refreshes", Face(Regular, 28), cx, cy+46, Center, cMid)
}

func drawLineChart(c *Canvas, v *View, x, top, w, h int, m provider.Meter) {
	drawChartTitle(c, x, top, w, m.Short+" usage · last 24 hours", "")
	px0, px1 := x+86, x+w-8
	py0, py1 := top+70, top+h-62
	plotH := float64(py1 - py0)
	start := v.Now.Add(-24 * time.Hour)
	xOf := func(t time.Time) float64 {
		return float64(px0) + t.Sub(start).Seconds()/(24*3600)*float64(px1-px0)
	}
	yOf := func(p float64) float64 { return float64(py1) - math.Min(p, 100)/100*plotH }

	// x-axis labels on whole 6-hour marks
	lt := start.In(v.Loc)
	for mark := time.Date(lt.Year(), lt.Month(), lt.Day(), (lt.Hour()/6+1)*6, 0, 0, 0, v.Loc); mark.Before(v.Now); mark = mark.Add(6 * time.Hour) {
		mx := int(xOf(mark))
		if mx > px1-90 {
			break
		}
		c.Rect(mx-1, py1, 3, 12, cDark)
		c.Text(mark.Format("15:04"), Face(Regular, 26), mx, py1+44, Center, cDark)
	}
	c.Text("now", Face(Medium, 26), px1, py1+44, Right, cDark)

	pts := v.Hist.Series(m.ID, start)
	if len(pts) < 2 {
		drawEmpty(c, px0, py0, px1, py1)
		return
	}
	for _, g := range []float64{50, 100} {
		y := py1 - int(g/100*plotH)
		c.DashH(px0, px1, y, 2, 10, 8, cMid)
		c.Text(fmt.Sprintf("%.0f%%", g), Face(Regular, 26), px0-14, y+9, Right, cDark)
	}
	c.Text("0", Face(Regular, 26), px0-14, py1+9, Right, cDark)

	const maxGap = 20 * time.Minute
	for pass := 0; pass < 2; pass++ { // area first, then the line on top
		for i := 1; i < len(pts); i++ {
			a, b := pts[i-1], pts[i]
			if b.T.Sub(a.T) > maxGap {
				continue
			}
			ax, ay, bx, by := xOf(a.T), yOf(a.U), xOf(b.T), yOf(b.U)
			if pass == 1 {
				c.Line(ax, ay, bx, by, 5, cBlack)
				continue
			}
			for xx := int(ax); xx <= int(bx); xx++ {
				f := 0.0
				if bx > ax {
					f = (float64(xx) - ax) / (bx - ax)
				}
				yy := int(ay + (by-ay)*f)
				c.Rect(xx, yy, 1, py1-yy, cLight)
			}
		}
	}
	c.Rect(px0, py1, px1-px0, 3, cBlack)
}

// drawPerDayBars: how much of a multi-day limit was used each day, against an even spread.
func drawPerDayBars(c *Canvas, v *View, x, top, w, h int, m provider.Meter) {
	days := v.Hist.UsedPerDay(m.ID, v.Now, v.Loc, 7)
	total := 0.0
	for _, d := range days {
		total += d.Value
	}
	even := 100 / (m.Window.Hours() / 24)
	name, _, _ := strings.Cut(m.Title, " · ")
	drawBars(c, v, x, top, w, h, name+" used per day", fmt.Sprintf("avg %.1f%%/day", total/7), days, even)
}

// drawPeakBars is used when the provider has no multi-day limit.
func drawPeakBars(c *Canvas, v *View, x, top, w, h int, m provider.Meter) {
	drawBars(c, v, x, top, w, h, "Daily peak · "+m.Short, "", v.Hist.PeakPerDay(m.ID, v.Now, v.Loc, 7), 0)
}

func drawBars(c *Canvas, v *View, x, top, w, h int, title, sub string, days []history.DayValue, even float64) {
	drawChartTitle(c, x, top, w, title, sub)
	px0, px1 := x+4, x+w-4
	py0, py1 := top+80, top+h-62
	plotH := float64(py1 - py0)
	if len(v.Hist.Samples) < 2 {
		drawEmpty(c, px0, py0, px1, py1)
		return
	}
	scale := math.Max(even*1.6, 10)
	for _, d := range days {
		scale = math.Max(scale, d.Value*1.12)
	}
	if even > 0 {
		ey := py1 - int(even/scale*plotH)
		c.DashH(px0, px1, ey, 3, 14, 10, cMid)
		c.Text(fmt.Sprintf("even pace %.0f%%", even), Face(Regular, 24), px1, ey-10, Right, cDark)
	}
	slot := float64(px1-px0) / float64(len(days))
	bw := int(slot * 0.58)
	for i, d := range days {
		cx := px0 + int(slot*(float64(i)+0.5))
		bh := int(d.Value / scale * plotH)
		isToday := i == len(days)-1
		col := uint8(cDark)
		if isToday {
			col = cBlack
		}
		if bh > 0 {
			c.Rect(cx-bw/2, py1-bh, bw, bh, col)
		}
		label := fmt.Sprintf("%.0f%%", d.Value)
		if d.Value > 0 && d.Value < 1 {
			label = "<1%"
		}
		c.Text(label, Face(Bold, 28), cx, py1-bh-12, Center, cBlack)
		day, wt := d.Day.Format("Mon"), Regular
		if isToday {
			day, wt = "Today", Bold
		}
		c.Text(day, Face(wt, 27), cx, py1+44, Center, cBlack)
	}
	c.Rect(px0, py1, px1-px0, 3, cBlack)
}

func fmtDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	m := int(d.Round(time.Minute).Minutes())
	switch {
	case m >= 48*60:
		return fmt.Sprintf("%dd %dh", m/(24*60), m%(24*60)/60)
	case m >= 60:
		return fmt.Sprintf("%dh %02dm", m/60, m%60)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

// ---- other screens ---------------------------------------------------------

// Message shows a centred title and explanation. The footer hint defaults to the menu gesture.
func Message(v *View, title, sub string, hint ...string) *Canvas {
	c := NewCanvas(W, H)
	drawHeader(c, v)
	c.Text(title, FitFace(Bold, 64, title, W-2*margin), W/2, H/2-10, Center, cBlack)
	f := Face(Regular, 34)
	for i, line := range wrap(sub, f, W-4*margin) {
		c.Text(line, f, W/2, H/2+60+i*46, Center, cDark)
	}
	h := "Hold 3 s: menu"
	if len(hint) > 0 {
		h = hint[0]
	}
	drawFooter(c, v, h)
	return c
}

// Login shows a sign-in QR (when the provider has a sign-in page) and a QR for the
// phone page where the user pastes the code or key.
func Login(v *View, p provider.LoginPrompt, pasteURL, codePath, status string) *Canvas {
	c := NewCanvas(W, H)
	c.Text("Connect your "+v.Provider+" account", Face(Bold, 58), margin, 100, Left, cBlack)
	c.Rect(margin, 130, W-2*margin, 4, cBlack)

	colW := (W - 3*margin) / 2
	const qr = 560
	drawStep := func(x int, n, title, l1, l2, content string) {
		c.RoundRect(x, 170, 64, 64, 32, cBlack)
		c.Text(n, Face(Bold, 40), x+32, 216, Center, cWhite)
		c.Text(title, FitFace(Bold, 40, title, colW-90), x+86, 216, Left, cBlack)
		qx := x + (colW-qr)/2
		if content != "" {
			drawQR(c, content, qx, 262, qr)
		} else {
			c.StrokeRect(qx, 262, qr, qr, 4, cMid)
			c.Text("Waiting for Wi-Fi…", Face(Medium, 36), qx+qr/2, 262+qr/2, Center, cMid)
		}
		c.Text(l1, FitFace(Regular, 32, l1, colW), x+colW/2, 880, Center, cDark)
		c.Text(l2, FitFace(Regular, 32, l2, colW), x+colW/2, 926, Center, cDark)
	}
	if p.UserCode != "" {
		drawStep(margin, "1", "Scan to open "+Host(p.SignInURL), p.SignInHint, "", p.SignInURL)
		x := margin*2 + colW
		c.RoundRect(x, 170, 64, 64, 32, cBlack)
		c.Text("2", Face(Bold, 40), x+32, 216, Center, cWhite)
		c.Text("Enter this code", Face(Bold, 40), x+86, 216, Left, cBlack)
		c.RoundRect(x, 400, colW, 220, 28, cBlack)
		c.RoundRect(x+6, 406, colW-12, 208, 22, cWhite)
		c.Text(p.UserCode, FitFace(Bold, 110, p.UserCode, colW-60), x+colW/2, 552, Center, cBlack)
		c.Text("The Kindle connects by itself once you approve.", FitFace(Regular, 32, "The Kindle connects by itself once you approve.", colW), x+colW/2, 700, Center, cDark)
		c.Text("Codes expire after about 15 minutes.", Face(Regular, 30), x+colW/2, 750, Center, cMid)
	} else {
		drawPasteSteps(c, v, p, pasteURL, codePath, colW, drawStep)
	}

	c.Rect(margin, 980, W-2*margin, 2, cLight)
	c.Text(status, FitFace(Bold, 38, status, W-2*margin), W/2, 1060, Center, cBlack)
	c.Text("Read-only: the dashboard only reads your usage.", Face(Regular, 28), W/2, 1112, Center, cMid)
	drawFooter(c, v, "Hold 3 s: menu")
	return c
}

func drawPasteSteps(c *Canvas, v *View, p provider.LoginPrompt, pasteURL, codePath string, colW int,
	drawStep func(x int, n, title, l1, l2, content string)) {
	pasteLabel := "Phone must be on the same Wi-Fi"
	if pasteURL != "" {
		pasteLabel = strings.TrimPrefix(pasteURL, "http://")
	}
	pasteX, n := margin*2+colW, "2"
	if p.SignInURL == "" {
		pasteX, n = (W-colW)/2, "1"
	} else {
		drawStep(margin, "1", "Scan to open "+Host(p.SignInURL), p.SignInHint, "", p.SignInURL)
	}
	drawStep(pasteX, n, "Scan to paste "+p.InputLabel, pasteLabel, "or save it as "+codePath+" over USB", pasteURL)
}

// Host is a URL's host without "www.", for showing where a QR code leads.
func Host(u string) string {
	if pu, err := url.Parse(u); err == nil && pu.Host != "" {
		return strings.TrimPrefix(pu.Host, "www.")
	}
	return u
}

func drawQR(c *Canvas, content string, x, y, size int) {
	q, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		c.Text("QR error", Face(Bold, 36), x+size/2, y+size/2, Center, cBlack)
		return
	}
	q.DisableBorder = true
	bm := q.Bitmap()
	mod := size / len(bm)
	off := (size - mod*len(bm)) / 2
	for r := range bm {
		for col := range bm[r] {
			if bm[r][col] {
				c.Rect(x+off+col*mod, y+off+r*mod, mod, mod, cBlack)
			}
		}
	}
}

func wrap(s string, f font.Face, maxW int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		try := strings.TrimSpace(cur + " " + word)
		if TextWidth(f, try) > maxW && cur != "" {
			lines = append(lines, cur)
			cur = word
		} else {
			cur = try
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

type MenuItem struct {
	Label string
	Sub   string
}

// Menu draws big buttons in a two-column grid (an odd last item spans the full width)
// and returns each button's rectangle on the landscape canvas for hit-testing.
func Menu(v *View, title string, items []MenuItem, status string) (*Canvas, []image.Rectangle) {
	c := NewCanvas(W, H)
	c.Text(title, Face(Bold, 60), margin, 100, Left, cBlack)
	if status != "" {
		c.Text(status, Face(Medium, 36), W-margin, 98, Right, cBlack)
	}
	c.Rect(margin, 130, W-2*margin, 4, cBlack)

	const gap, top, bottom = 36, 176, H - 100
	rows := (len(items) + 1) / 2
	bh := min(300, (bottom-top-(rows-1)*gap)/rows)
	colW := (W - 2*margin - gap) / 2
	rects := make([]image.Rectangle, len(items))
	for i, it := range items {
		x, y, w := margin+(i%2)*(colW+gap), top+(i/2)*(bh+gap), colW
		if i == len(items)-1 && i%2 == 0 {
			w = W - 2*margin
		}
		c.RoundRect(x, y, w, bh, 28, cBlack)
		c.RoundRect(x+6, y+6, w-12, bh-12, 22, cWhite)
		ly := y + bh/2 + 20
		if it.Sub != "" {
			ly -= 22
			c.Text(it.Sub, FitFace(Regular, 30, it.Sub, w-40), x+w/2, ly+56, Center, cDark)
		}
		c.Text(it.Label, FitFace(Bold, 56, it.Label, w-40), x+w/2, ly, Center, cBlack)
		rects[i] = image.Rect(x, y, x+w, y+bh)
	}
	c.Text("Tap a button   ·   closes by itself after 20 s", Face(Regular, 27), margin, H-40, Left, cMid)
	return c, rects
}
