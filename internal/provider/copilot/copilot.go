// Package copilot reads GitHub Copilot's monthly quotas (premium requests, and chat /
// completions on the free plan).
//
// It logs in with GitHub's device flow using the Copilot editor plugins' public OAuth
// client, and reads api.github.com/copilot_internal/user, the endpoint editors use to
// show remaining quota. Neither is a documented public API.
package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prat31/kindle-ai-tracker/internal/provider"
)

const (
	clientID  = "Iv1.b507a08c87ecfe98"
	deviceURL = "https://github.com/login/device/code"
	tokenURL  = "https://github.com/login/oauth/access_token"
	usageURL  = "https://api.github.com/copilot_internal/user"
)

func init() { provider.Register("copilot", New) }

type stored struct {
	Token string `json:"token"`
}

type Copilot struct {
	dir        string
	token      string
	deviceCode string
}

func New(dir string, _ json.RawMessage) (provider.Provider, error) {
	c := &Copilot{dir: dir}
	var s stored
	if provider.LoadJSON(c.path(), &s) {
		c.token = s.Token
	}
	return c, nil
}

func (c *Copilot) ID() string     { return "copilot" }
func (c *Copilot) Name() string   { return "GitHub Copilot" }
func (c *Copilot) LoggedIn() bool { return c.token != "" }
func (c *Copilot) path() string   { return filepath.Join(c.dir, "tokens.json") }
func (c *Copilot) Logout()        { c.token = ""; os.Remove(c.path()) }
func (c *Copilot) CompleteLogin(context.Context, string) error {
	return errors.New("Copilot logs in with a device code, nothing to paste")
}

func postForm(ctx context.Context, u string, v url.Values, out any) error {
	raw, err := provider.Do(ctx, "POST", u, strings.NewReader(v.Encode()),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (c *Copilot) BeginLogin(ctx context.Context) (provider.LoginPrompt, error) {
	var r struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
	}
	if err := postForm(ctx, deviceURL, url.Values{"client_id": {clientID}, "scope": {"read:user"}}, &r); err != nil {
		return provider.LoginPrompt{}, err
	}
	if r.DeviceCode == "" || r.UserCode == "" {
		return provider.LoginPrompt{}, errors.New("unexpected device code response")
	}
	c.deviceCode = r.DeviceCode
	if r.VerificationURI == "" {
		r.VerificationURI = "https://github.com/login/device"
	}
	return provider.LoginPrompt{
		SignInURL:  r.VerificationURI,
		SignInHint: "Sign in to GitHub and enter the code",
		UserCode:   r.UserCode,
		PollEvery:  time.Duration(max(r.Interval, 5)+1) * time.Second,
	}, nil
}

func (c *Copilot) PollLogin(ctx context.Context) (bool, error) {
	if c.deviceCode == "" {
		return false, errors.New("login not started")
	}
	var r struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	err := postForm(ctx, tokenURL, url.Values{
		"client_id":   {clientID},
		"device_code": {c.deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}, &r)
	switch {
	case err != nil:
		return false, err
	case r.Error == "authorization_pending" || r.Error == "slow_down":
		return false, nil
	case r.Error != "":
		return false, fmt.Errorf("%s: %s", r.Error, r.Description)
	case r.AccessToken == "":
		return false, errors.New("unexpected token response")
	}
	c.token, c.deviceCode = r.AccessToken, ""
	return true, provider.SaveJSON(c.path(), stored{Token: c.token})
}

func (c *Copilot) Fetch(ctx context.Context) (*provider.Snapshot, []byte, error) {
	if !c.LoggedIn() {
		return nil, nil, provider.ErrReauth
	}
	raw, err := provider.Do(ctx, "GET", usageURL, nil, map[string]string{
		"Authorization":  "token " + c.token,
		"Editor-Version": "kindle-ai-tracker/0.3",
	})
	if provider.IsStatus(err, 401) {
		return nil, raw, fmt.Errorf("%w: %v", provider.ErrReauth, err)
	}
	if err != nil {
		return nil, raw, err
	}
	snap, err := Parse(raw)
	if err != nil {
		return nil, raw, err
	}
	snap.Fetched = time.Now()
	return snap, raw, nil
}

type quota struct {
	Entitlement      float64 `json:"entitlement"`
	Remaining        float64 `json:"remaining"`
	PercentRemaining float64 `json:"percent_remaining"`
	OverageCount     float64 `json:"overage_count"`
	Unlimited        bool    `json:"unlimited"`
}

func Parse(raw []byte) (*provider.Snapshot, error) {
	var p struct {
		Plan      string           `json:"copilot_plan"`
		ResetDate string           `json:"quota_reset_date"`
		Snapshots map[string]quota `json:"quota_snapshots"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("usage response is not JSON: %w", err)
	}
	reset := parseDate(p.ResetDate)
	var window time.Duration
	if !reset.IsZero() {
		window = reset.Sub(reset.AddDate(0, -1, 0))
	}
	snap := &provider.Snapshot{Plan: provider.Capitalize(p.Plan)}
	for _, k := range []struct{ key, title, short string }{
		{"premium_interactions", "Premium requests · monthly", "Premium"},
		{"chat", "Chat messages · monthly", "Chat"},
		{"completions", "Code completions · monthly", "Completions"},
	} {
		q, ok := p.Snapshots[k.key]
		if !ok || q.Unlimited || q.Entitlement <= 0 {
			continue
		}
		detail := fmt.Sprintf("%.0f of %.0f left", max(q.Remaining, 0), q.Entitlement)
		if q.OverageCount > 0 {
			detail += fmt.Sprintf(", %.0f over", q.OverageCount)
		}
		snap.Meters = append(snap.Meters, provider.Meter{ID: k.key, Title: k.title, Short: k.short,
			Used: 100 - q.PercentRemaining, Window: window, ResetsAt: reset, Detail: detail})
	}
	if len(snap.Meters) == 0 {
		return nil, errors.New("no limited Copilot quotas on this account")
	}
	return snap, nil
}

func parseDate(s string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
