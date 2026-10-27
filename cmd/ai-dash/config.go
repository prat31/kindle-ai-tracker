package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/prat31/kindle-ai-tracker/internal/device"
)

// Config lives in <dir>/config.json. Missing fields keep their defaults.
type Config struct {
	// Which side the Kindle's USB-C port is on when it stands in landscape:
	// "usb-right" or "usb-left".
	Orientation    string `json:"orientation"`
	RefreshMinutes int    `json:"refresh_minutes"`
	// Stop auto-refreshing after this many fetches in a row brought nothing new
	// (no change, or an error); a tap resumes. 0 never pauses.
	PauseAfter       int    `json:"pause_after"`
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

	loc *time.Location
}

func defaultConfig() Config {
	return Config{
		Orientation:      "usb-right",
		RefreshMinutes:   5,
		PauseAfter:       6,
		FullRefreshEvery: 6,
		FBInk:            "/mnt/us/libkh/bin/fbink",
		LoginPort:        8642,
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
		cfg.RefreshMinutes = 5
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
	return cfg
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
