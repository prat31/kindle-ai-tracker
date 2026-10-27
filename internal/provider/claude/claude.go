// Package claude reads Claude Pro/Max plan limits (5-hour session, weekly, per-model weekly).
//
// It logs in with the same public OAuth client and copy-the-code flow Claude Code uses
// for "Log in with Claude.ai", and reads the usage endpoint behind Claude Code's /usage
// screen. Verified against Claude Code 2.1.308. Neither is a documented public API.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prat31/kindle-ai-tracker/internal/oauth"
	"github.com/prat31/kindle-ai-tracker/internal/provider"
)

const (
	clientID     = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	authorizeURL = "https://claude.com/cai/oauth/authorize"
	tokenURL     = "https://platform.claude.com/v1/oauth/token"
	redirectURL  = "https://platform.claude.com/oauth/code/callback"
	usageURL     = "https://api.anthropic.com/api/oauth/usage"
	betaHeader   = "oauth-2025-04-20"
)

func init() { provider.Register("claude", New) }

type Options struct {
	PlanLabel string `json:"plan_label"`
	Scopes    string `json:"scopes"`
}

type Claude struct {
	opts   Options
	dir    string
	tokens *oauth.Tokens
	login  *oauth.PKCE
}

func New(dir string, raw json.RawMessage) (provider.Provider, error) {
	opts := Options{PlanLabel: "Pro", Scopes: "user:profile user:inference"}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &opts); err != nil {
			return nil, fmt.Errorf("claude options: %w", err)
		}
	}
	c := &Claude{opts: opts, dir: dir}
	c.tokens = oauth.LoadTokens(c.tokenPath())
	return c, nil
}

func (c *Claude) ID() string        { return "claude" }
func (c *Claude) Name() string      { return "Claude" }
func (c *Claude) LoggedIn() bool    { return c.tokens != nil && c.tokens.RefreshToken != "" }
func (c *Claude) tokenPath() string { return filepath.Join(c.dir, "tokens.json") }

func (c *Claude) Logout() {
	c.tokens = nil
	os.Remove(c.tokenPath())
}

func (c *Claude) BeginLogin(context.Context) (provider.LoginPrompt, error) {
	c.login = oauth.NewPKCE()
	q := url.Values{}
	q.Set("code", "true")
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURL)
	q.Set("scope", c.opts.Scopes)
	q.Set("code_challenge", c.login.Challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", c.login.State)
	return provider.LoginPrompt{
		SignInURL:  authorizeURL + "?" + q.Encode(),
		SignInHint: "Approve access, then copy the code Claude shows you",
		InputLabel: "the code Claude showed you",
	}, nil
}

func (c *Claude) CompleteLogin(ctx context.Context, input string) error {
	if c.login == nil {
		return errors.New("login not started")
	}
	code, state := oauth.ParseCodeInput(input)
	if code == "" {
		return errors.New("no code found in what was pasted")
	}
	if state != "" && state != c.login.State {
		return errors.New("that code belongs to a different login attempt; scan the QR again")
	}
	t, err := oauth.RequestTokens(ctx, tokenURL, map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"redirect_uri":  redirectURL,
		"client_id":     clientID,
		"code_verifier": c.login.Verifier,
		"state":         c.login.State,
	}, "")
	if err != nil {
		return err
	}
	c.tokens, c.login = t, nil
	return oauth.SaveTokens(c.tokenPath(), t)
}

func (c *Claude) refresh(ctx context.Context) error {
	t, err := oauth.RequestTokens(ctx, tokenURL, map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": c.tokens.RefreshToken,
		"client_id":     clientID,
		"scope":         c.opts.Scopes,
	}, c.tokens.RefreshToken)
	var he *provider.HTTPError
	if errors.As(err, &he) && (he.Status == 400 || he.Status == 401 || he.Status == 403) {
		return fmt.Errorf("%w: %v", provider.ErrReauth, err)
	}
	if err != nil {
		return err
	}
	c.tokens = t
	return oauth.SaveTokens(c.tokenPath(), t)
}

func (c *Claude) Fetch(ctx context.Context) (*provider.Snapshot, []byte, error) {
	if !c.LoggedIn() {
		return nil, nil, provider.ErrReauth
	}
	if c.tokens.ExpiresWithin(2 * time.Minute) {
		if err := c.refresh(ctx); err != nil {
			return nil, nil, err
		}
	}
	raw, err := c.get(ctx)
	var he *provider.HTTPError
	if errors.As(err, &he) && he.Status == 401 {
		if err := c.refresh(ctx); err != nil {
			return nil, raw, err
		}
		raw, err = c.get(ctx)
	}
	if err != nil {
		return nil, raw, err
	}
	snap, err := Parse(raw)
	if err != nil {
		return nil, raw, err
	}
	snap.Plan = c.opts.PlanLabel
	snap.Fetched = time.Now()
	return snap, raw, nil
}

func (c *Claude) get(ctx context.Context) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", usageURL, nil)
	req.Header.Set("Authorization", "Bearer "+c.tokens.AccessToken)
	req.Header.Set("anthropic-beta", betaHeader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", oauth.UserAgent)
	resp, err := oauth.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return raw, &provider.HTTPError{Status: resp.StatusCode, Body: string(raw)}
	}
	return raw, nil
}

const (
	week    = 7 * 24 * time.Hour
	session = 5 * time.Hour
)

// Parse understands both shapes Claude Code reads: the newer "limits" array and the
// older per-window objects (five_hour, seven_day, seven_day_opus, ...).
func Parse(raw []byte) (*provider.Snapshot, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("usage response is not JSON: %w", err)
	}
	var sess, weekly *provider.Meter
	var scoped []provider.Meter

	if lr, ok := top["limits"]; ok {
		var limits []struct {
			Kind     string          `json:"kind"`
			Percent  *float64        `json:"percent"`
			ResetsAt json.RawMessage `json:"resets_at"`
			Scope    *struct {
				Model *struct {
					DisplayName string `json:"display_name"`
				} `json:"model"`
				Surface *struct {
					DisplayName string `json:"display_name"`
				} `json:"surface"`
			} `json:"scope"`
		}
		if json.Unmarshal(lr, &limits) == nil {
			for _, l := range limits {
				if l.Percent == nil {
					continue
				}
				reset := parseTime(l.ResetsAt)
				switch l.Kind {
				case "session":
					sess = sessionMeter(*l.Percent, reset)
				case "weekly_all":
					weekly = weeklyMeter(*l.Percent, reset)
				case "weekly_scoped":
					name := "Scoped"
					if l.Scope != nil && l.Scope.Model != nil {
						name = l.Scope.Model.DisplayName
					} else if l.Scope != nil && l.Scope.Surface != nil {
						name = l.Scope.Surface.DisplayName
					}
					scoped = append(scoped, scopedMeter(name, *l.Percent, reset))
				}
			}
		}
	}

	legacy := func(key string) (float64, time.Time, bool) {
		r, ok := top[key]
		if !ok || string(r) == "null" {
			return 0, time.Time{}, false
		}
		var o struct {
			Utilization *float64        `json:"utilization"`
			ResetsAt    json.RawMessage `json:"resets_at"`
		}
		if json.Unmarshal(r, &o) != nil || o.Utilization == nil {
			return 0, time.Time{}, false
		}
		return *o.Utilization, parseTime(o.ResetsAt), true
	}
	if sess == nil {
		if p, t, ok := legacy("five_hour"); ok {
			sess = sessionMeter(p, t)
		}
	}
	if weekly == nil {
		if p, t, ok := legacy("seven_day"); ok {
			weekly = weeklyMeter(p, t)
		}
	}
	if len(scoped) == 0 {
		for _, k := range []struct{ key, name string }{{"seven_day_opus", "Opus"}, {"seven_day_sonnet", "Sonnet"}} {
			if p, t, ok := legacy(k.key); ok && !t.IsZero() {
				scoped = append(scoped, scopedMeter(k.name, p, t))
			}
		}
	}
	sort.SliceStable(scoped, func(i, j int) bool { return scoped[i].Used > scoped[j].Used })

	snap := &provider.Snapshot{}
	for _, m := range []*provider.Meter{sess, weekly} {
		if m != nil {
			snap.Meters = append(snap.Meters, *m)
		}
	}
	snap.Meters = append(snap.Meters, scoped...)
	if len(snap.Meters) == 0 {
		return nil, fmt.Errorf("usage response has no limits we recognise")
	}
	return snap, nil
}

func sessionMeter(p float64, reset time.Time) *provider.Meter {
	return &provider.Meter{ID: "session", Title: "Current session · 5-hour limit", Short: "Session",
		Used: p, Window: session, ResetsAt: reset}
}

func weeklyMeter(p float64, reset time.Time) *provider.Meter {
	return &provider.Meter{ID: "weekly", Title: "Weekly limit · all models", Short: "Weekly",
		Used: p, Window: week, ResetsAt: reset}
}

func scopedMeter(name string, p float64, reset time.Time) provider.Meter {
	return provider.Meter{ID: "weekly_" + strings.ToLower(strings.ReplaceAll(name, " ", "_")),
		Title: "Weekly limit · " + name, Short: name, Used: p, Window: week, ResetsAt: reset}
}

func parseTime(r json.RawMessage) time.Time {
	s := strings.Trim(strings.TrimSpace(string(r)), `"`)
	if s == "" || s == "null" {
		return time.Time{}
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		if n > 1e12 { // milliseconds
			return time.UnixMilli(int64(n))
		}
		return time.Unix(int64(n), 0)
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
