package render

import (
	"strings"
	"testing"
	"time"

	"github.com/prat31/kindle-ai-tracker/internal/history"
	"github.com/prat31/kindle-ai-tracker/internal/provider"
)

// The session window ended hours ago and nothing was used since: the service still
// reports the old window. This used to print "At this pace: -97% when it resets".
func TestPaceNeverNegative(t *testing.T) {
	now := time.Now()
	hist := &history.Store{}
	for i := 6; i >= 0; i-- {
		hist.Add(history.Sample{T: now.Add(-time.Duration(i) * 10 * time.Minute).Unix(),
			M: map[string]history.Point{"session": {U: 22, R: now.Add(-4 * time.Hour).Unix()}}})
	}
	v := &View{Now: now, Loc: time.UTC, Hist: hist}
	for _, m := range []provider.Meter{
		{ID: "session", Used: 22, Window: 5 * time.Hour, ResetsAt: now.Add(10 * time.Minute)},
		{ID: "session", Used: 99, Window: 5 * time.Hour, ResetsAt: now.Add(4 * time.Hour)},
	} {
		l1, l2 := paceLines(v, m)
		if strings.Contains(l1+l2, "-") && !strings.Contains(l1, "—") {
			t.Errorf("negative number in %q / %q", l1, l2)
		}
	}
	c := NewCanvas(W, H)
	drawMeterPanel(c, v, 0, 0, 700, provider.Meter{ID: "session", Used: 22, Window: 5 * time.Hour, ResetsAt: now.Add(-4 * time.Hour)}, "")
}
