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
	for _, c := range []struct {
		name string
		a    App
		want bool
	}{
		{"updating", App{autoSleep: true, connected: []*source{src}, awakeUntil: past}, false},
		{"paused", App{autoSleep: true, paused: true, connected: []*source{src}, awakeUntil: past}, true},
		{"paused, just touched", App{autoSleep: true, paused: true, connected: []*source{src}, awakeUntil: future}, false},
		{"paused, sleep off", App{autoSleep: false, paused: true, connected: []*source{src}, awakeUntil: past}, false},
		{"paused, powerd refused lately", App{autoSleep: true, paused: true, connected: []*source{src}, awakeUntil: past, retryAt: future}, false},
		{"nothing connected", App{autoSleep: true, awakeUntil: past}, true},
		{"power button, sleep off", App{sleepNow: true, awakeUntil: future, retryAt: future}, true},
		{"power button off a Kindle", App{sleepNow: true, noSuspend: true}, false},
	} {
		if got := c.a.shouldSleep(); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
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
