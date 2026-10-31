package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prat31/kindle-ai-tracker/internal/device"
)

// Config lives in <dir>/config.json. Missing fields keep their defaults.
type Config struct {
	// Which side the Kindle's USB-C port is on when it stands in landscape:
	// "usb-right" or "usb-left".
	Orientation    string `json:"orientation"`
	RefreshMinutes int    `json:"refresh_minutes"`
	// With sleep_between_updates off, wait up to this long between updates once nothing
	// has changed for a while (see PauseAfter); the wait doubles towards it.
	MaxRefreshMinutes int `json:"max_refresh_minutes"`
	// Stop auto-refreshing, and sleep, after this many fetches in a row brought nothing
	// new (no change, or an error); a tap or the power button resumes. 0 never pauses.
	PauseAfter int `json:"pause_after"`
	// Once auto-refresh pauses (see PauseAfter), sleep without fetching until the power
	// button is pressed. When false (or the header snooze button is crossed out) it never
	// pauses and keeps updating, less often while nothing changes (MaxRefreshMinutes).
	// Either way the Kindle sleeps between updates when it isn't being used.
	SleepBetweenUpdates bool `json:"sleep_between_updates"`
	// No updates between these times, e.g. "23:00-07:00": the Kindle sleeps through
	// them and updates when they end. "" updates around the clock.
	QuietHours string `json:"quiet_hours"`
	// Front light level while the dashboard runs (0 = off, -1 = leave it as it is).
	Frontlight       int    `json:"frontlight"`
	FullRefreshEvery int    `json:"full_refresh_every"`
	Timezone         string `json:"timezone"`
	FBInk            string `json:"fbink"`
	LoginPort        int    `json:"login_port"`
	// Touchscreen axis fixes, if menu taps land on the wrong button.
	device.Calibration
	// Optional per-provider options: {"id": "<provider>", ...options}. Which providers
	// are shown is chosen on the Kindle (menu → Connectors) and kept in state.json.
	Providers []json.RawMessage `json:"providers"`

	PlanLabel string `json:"plan_label"` // pre-0.2 configs: Claude's plan label at the top level

	loc                   *time.Location
	quietFrom, quietUntil int // minutes after midnight; equal when there are no quiet hours
}

func defaultConfig() Config {
	return Config{
		Orientation:         "usb-right",
		RefreshMinutes:      30,
		MaxRefreshMinutes:   120,
		QuietHours:          "23:00-07:00",
		PauseAfter:          6,
		SleepBetweenUpdates: true,
		Frontlight:          0,
		FullRefreshEvery:    6,
		FBInk:               "/mnt/us/libkh/bin/fbink",
		LoginPort:           8642,
	}
}

func loadConfig(dir string) Config {
	cfg := defaultConfig()
	if b, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			log.Printf("config.json is invalid, using defaults: %v", err)
			cfg = defaultConfig()
		}
	}
	if cfg.RefreshMinutes < 1 {
		cfg.RefreshMinutes = 30
	}
	if cfg.MaxRefreshMinutes < cfg.RefreshMinutes {
		cfg.MaxRefreshMinutes = cfg.RefreshMinutes
	}
	if cfg.FullRefreshEvery < 1 {
		cfg.FullRefreshEvery = 6
	}
	cfg.loc = time.Local
	if cfg.Timezone != "" {
		if loc, err := time.LoadLocation(cfg.Timezone); err == nil {
			cfg.loc = loc
		} else {
			log.Printf("unknown timezone %q: %v", cfg.Timezone, err)
		}
	}
	if cfg.QuietHours != "" {
		from, until, ok := parseQuietHours(cfg.QuietHours)
		if !ok {
			log.Printf("quiet_hours %q should look like \"23:00-07:00\"; ignoring it", cfg.QuietHours)
		}
		cfg.quietFrom, cfg.quietUntil = from, until
	}
	return cfg
}

// parseQuietHours reads "HH:MM-HH:MM" into minutes after midnight.
func parseQuietHours(s string) (from, until int, ok bool) {
	a, b, found := strings.Cut(s, "-")
	if !found {
		return 0, 0, false
	}
	ta, errA := time.Parse("15:04", strings.TrimSpace(a))
	tb, errB := time.Parse("15:04", strings.TrimSpace(b))
	if errA != nil || errB != nil {
		return 0, 0, false
	}
	return ta.Hour()*60 + ta.Minute(), tb.Hour()*60 + tb.Minute(), true
}

// quietEnd reports whether t falls in the quiet hours and, if so, when they end.
func (c Config) quietEnd(t time.Time) (time.Time, bool) {
	if c.quietFrom == c.quietUntil {
		return time.Time{}, false
	}
	lt := t.In(c.loc)
	m := lt.Hour()*60 + lt.Minute()
	in := m >= c.quietFrom && m < c.quietUntil
	if c.quietFrom > c.quietUntil { // across midnight
		in = m >= c.quietFrom || m < c.quietUntil
	}
	if !in {
		return time.Time{}, false
	}
	end := time.Date(lt.Year(), lt.Month(), lt.Day(), c.quietUntil/60, c.quietUntil%60, 0, 0, c.loc)
	if !end.After(lt) {
		end = end.AddDate(0, 0, 1)
	}
	return end, true
}

// optionsFor returns the provider's entry from config.json, or nil.
func (c Config) optionsFor(id string) json.RawMessage {
	for _, entry := range c.Providers {
		var e struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(entry, &e) == nil && e.ID == id {
			return entry
		}
	}
	if id == "claude" && c.PlanLabel != "" {
		b, _ := json.Marshal(map[string]string{"plan_label": c.PlanLabel})
		return b
	}
	return nil
}
