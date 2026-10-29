// Package opencode reads the OpenCode Go plan limits: the rolling 5-hour, weekly and
// monthly spend caps. It uses GET /zen/go/v1/usage with the Go API key, which isn't
// documented publicly, so the parser accepts a few spellings of each field.
package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/prat31/kindle-ai-tracker/internal/provider"
)

const usageURL = "https://opencode.ai/zen/go/v1/usage"

func init() { provider.Register("opencode", New) }

type OpenCode struct {
	dir string
	key string
}

func New(dir string, _ json.RawMessage) (provider.Provider, error) {
	o := &OpenCode{dir: dir}
	var k provider.APIKey
	if provider.LoadJSON(o.path(), &k) {
		o.key = k.Key
	}
	return o, nil
}

func (o *OpenCode) ID() string     { return "opencode" }
func (o *OpenCode) Name() string   { return "OpenCode Go" }
func (o *OpenCode) LoggedIn() bool { return o.key != "" }
func (o *OpenCode) path() string   { return filepath.Join(o.dir, "key.json") }
func (o *OpenCode) Logout()        { o.key = ""; os.Remove(o.path()) }

func (o *OpenCode) BeginLogin(context.Context) (provider.LoginPrompt, error) {
	return provider.LoginPrompt{
		SignInURL:  "https://opencode.ai/auth",
		SignInHint: "Open Go in your workspace and copy its API key",
		InputLabel: "your OpenCode Go API key",
	}, nil
}

func (o *OpenCode) CompleteLogin(ctx context.Context, input string) error {
	key := strings.TrimSpace(input)
	raw, err := get(ctx, key)
	if err != nil {
		if provider.IsStatus(err, 401, 403) {
			return errors.New("OpenCode didn't accept that key (is it a Go plan key?)")
		}
		return err
	}
	if _, err := Parse(raw, time.Now()); err != nil {
		return err
	}
	o.key = key
	return provider.SaveJSON(o.path(), provider.APIKey{Key: key})
}

func get(ctx context.Context, key string) ([]byte, error) {
	return provider.Do(ctx, "GET", usageURL, nil, map[string]string{"Authorization": "Bearer " + key})
}

func (o *OpenCode) Fetch(ctx context.Context) (*provider.Snapshot, []byte, error) {
	if !o.LoggedIn() {
		return nil, nil, provider.ErrReauth
	}
	raw, err := get(ctx, o.key)
	if provider.IsStatus(err, 401) {
		return nil, raw, fmt.Errorf("%w: %v", provider.ErrReauth, err)
	}
	if err != nil {
		return nil, raw, err
	}
	snap, err := Parse(raw, time.Now())
	if err != nil {
		return nil, raw, err
	}
	snap.Fetched = time.Now()
	return snap, raw, nil
}

// window is one limit as the endpoint reports it. The reset comes either as a time
// (ISO 8601, or Unix seconds or milliseconds) or as seconds from now.
type window struct {
	Percent      *float64        `json:"percent"`
	UsagePercent *float64        `json:"usagePercent"`
	ResetsAt     json.RawMessage `json:"resetsAt"`
	ResetInSec   *float64        `json:"resetInSec"`
}

func (w *window) used() (float64, bool) {
	switch {
	case w.Percent != nil:
		return *w.Percent, true
	case w.UsagePercent != nil:
		return *w.UsagePercent, true
	}
	return 0, false
}

func (w *window) resets(now time.Time) time.Time {
	if w.ResetInSec != nil {
		// Rounded so every reading of one window has the same reset time: history takes a
		// changed reset for a new window.
		return now.Add(time.Duration(*w.ResetInSec * float64(time.Second))).Round(time.Minute)
	}
	s := strings.Trim(string(w.ResetsAt), `"`)
	if s == "" || s == "null" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil && n > 0 {
		if n > 1e12 {
			return time.UnixMilli(int64(n))
		}
		return time.Unix(int64(n), 0)
	}
	return time.Time{}
}

func Parse(raw []byte, now time.Time) (*provider.Snapshot, error) {
	type windows struct {
		Rolling *window `json:"rolling"`
		Weekly  *window `json:"weekly"`
		Monthly *window `json:"monthly"`
	}
	var env struct {
		Usage *windows `json:"usage"`
		windows
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("usage response is not JSON: %w", err)
	}
	w := env.windows
	if env.Usage != nil {
		w = *env.Usage
	}
	snap := &provider.Snapshot{} // no Plan: the name already says "Go"
	for _, l := range []struct {
		w                *window
		id, title, short string
		window           time.Duration
	}{
		{w.Rolling, "session", "Current session · 5-hour limit", "Session", 5 * time.Hour},
		{w.Weekly, "weekly", "Weekly limit", "Weekly", 7 * 24 * time.Hour},
		{w.Monthly, "monthly", "Monthly limit", "Monthly", 30 * 24 * time.Hour},
	} {
		if l.w == nil {
			continue
		}
		used, ok := l.w.used()
		if !ok {
			continue
		}
		snap.Meters = append(snap.Meters, provider.Meter{ID: l.id, Title: l.title, Short: l.short,
			Used: used, Window: l.window, ResetsAt: l.w.resets(now)})
	}
	if len(snap.Meters) == 0 {
		return nil, errors.New("OpenCode returned no Go usage limits")
	}
	return snap, nil
}
