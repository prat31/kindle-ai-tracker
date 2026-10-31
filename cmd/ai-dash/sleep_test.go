package main

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"
	"time"
)

func TestTrackIdle(t *testing.T) {
	a := &App{cfg: Config{PauseAfter: 6}, autoSleep: true, next: time.Now().Add(time.Minute)}
	for range 5 {
		a.trackIdle(false)
	}
	if a.paused {
		t.Fatal("paused after 5 unchanged updates")
	}
	a.trackIdle(false)
	if !a.paused || !a.next.IsZero() {
		t.Fatalf("not paused after 6 unchanged updates: %+v", a)
	}

	// With sleep turned off it keeps updating however long nothing changes.
	b := &App{cfg: Config{PauseAfter: 6}, autoSleep: false}
	for range 100 {
		b.trackIdle(false)
	}
	if b.paused {
		t.Fatal("paused with sleep turned off")
	}
}

func TestShouldSleep(t *testing.T) {
	src := &source{}
	past, future := time.Now().Add(-time.Minute), time.Now().Add(time.Minute)
	next := time.Now().Add(5 * time.Minute)
	for _, c := range []struct {
		name  string
		a     *App
		want  bool
		until time.Time // zero: until the power button
	}{
		{"updating", &App{autoSleep: true, connected: []*source{src}, awakeUntil: past, next: next}, true, next},
		{"updating, sleep off", &App{autoSleep: false, connected: []*source{src}, awakeUntil: past, next: next}, true, next},
		{"updating, just touched", &App{autoSleep: true, connected: []*source{src}, awakeUntil: future, next: next}, false, time.Time{}},
		{"paused", &App{autoSleep: true, paused: true, connected: []*source{src}, awakeUntil: past}, true, time.Time{}},
		{"paused, just touched", &App{autoSleep: true, paused: true, connected: []*source{src}, awakeUntil: future}, false, time.Time{}},
		{"powerd refused lately", &App{autoSleep: true, paused: true, connected: []*source{src}, awakeUntil: past, retryAt: future}, false, time.Time{}},
		{"nothing connected", &App{autoSleep: true, awakeUntil: past}, true, time.Time{}},
		{"power button, updating", &App{sleepNow: true, connected: []*source{src}, awakeUntil: future, retryAt: future, next: next}, true, next},
		{"power button, paused", &App{sleepNow: true, paused: true, connected: []*source{src}, awakeUntil: future}, true, time.Time{}},
		{"power button off a Kindle", &App{sleepNow: true, noSuspend: true}, false, time.Time{}},
	} {
		got, until := c.a.shouldSleep()
		if got != c.want || !until.Equal(c.until) {
			t.Errorf("%s: got %v until %v, want %v until %v", c.name, got, until, c.want, c.until)
		}
	}
}

func TestRefreshMinutes(t *testing.T) {
	a := &App{cfg: Config{RefreshMinutes: 5, MaxRefreshMinutes: 30, PauseAfter: 6}}
	for idle, want := range []int{5, 5, 5, 5, 5, 5, 10, 20, 30, 30, 30} {
		a.idle = idle
		if got := a.refreshMinutes(); got != want {
			t.Errorf("after %d unchanged: %d minutes, want %d", idle, got, want)
		}
	}
	a.cfg.PauseAfter, a.idle = 0, 50
	if got := a.refreshMinutes(); got != 5 {
		t.Errorf("pause_after 0: %d minutes, want 5", got)
	}
}

func TestOffline(t *testing.T) {
	dial := &url.Error{Op: "Get", URL: "https://x", Err: &net.OpError{Op: "dial", Err: errors.New("network is unreachable")}}
	dns := &url.Error{Op: "Get", URL: "https://x", Err: &net.DNSError{Err: "no such host", Name: "x"}}
	tls := &url.Error{Op: "Get", URL: "https://x", Err: x509.CertificateInvalidError{Reason: x509.Expired}}
	for _, c := range []struct {
		err  error
		want bool
	}{
		{dial, true},
		{fmt.Errorf("usage: %w", dns), true},
		{context.DeadlineExceeded, true},
		{tls, false},
		{errors.New("HTTP 500"), false},
	} {
		if got := offline(c.err); got != c.want {
			t.Errorf("offline(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestQuietHours(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+1800)
	at := func(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, loc) }
	from, until, ok := parseQuietHours("23:00-07:00")
	if !ok {
		t.Fatal("couldn't parse 23:00-07:00")
	}
	a := &App{cfg: Config{RefreshMinutes: 30, MaxRefreshMinutes: 120, loc: loc, quietFrom: from, quietUntil: until}}
	for _, c := range []struct {
		now, want time.Time
	}{
		{at(1, 22, 10), at(1, 22, 30)},
		{at(1, 22, 40), at(2, 7, 0)}, // 23:00 is quiet
		{at(2, 2, 0), at(2, 7, 0)},
		{at(2, 6, 50), at(2, 7, 0)},
		{at(2, 7, 0), at(2, 7, 30)},
	} {
		if got := a.nextUpdate(c.now); !got.Equal(c.want) {
			t.Errorf("after %v: next update %v, want %v", c.now, got, c.want)
		}
	}

	a.cfg.quietFrom, a.cfg.quietUntil = 0, 0 // none
	if got := a.nextUpdate(at(1, 23, 10)); !got.Equal(at(1, 23, 30)) {
		t.Errorf("without quiet hours: next update %v", got)
	}
	for _, bad := range []string{"23:00", "late-early", "25:00-07:00"} {
		if _, _, ok := parseQuietHours(bad); ok {
			t.Errorf("parsed %q", bad)
		}
	}
}
