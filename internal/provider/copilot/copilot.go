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

// stored is the GitHub login. Refresh and Expires are only set when GitHub issued an
// expiring token; otherwise the token lasts until it's revoked.
type stored struct {
	Token   string    `json:"token"`
	Refresh string    `json:"refresh_token,omitempty"`
	Expires time.Time `json:"expires,omitempty"`
}

type Copilot struct {
	dir        string
	tok        stored
	deviceCode string
}

func New(dir string, _ json.RawMessage) (provider.Provider, error) {
	c := &Copilot{dir: dir}
	provider.LoadJSON(c.path(), &c.tok)
	return c, nil
}

func (c *Copilot) ID() string     { return "copilot" }
func (c *Copilot) Name() string   { return "GitHub Copilot" }
func (c *Copilot) LoggedIn() bool { return c.tok.Token != "" }
func (c *Copilot) path() string   { return filepath.Join(c.dir, "tokens.json") }
func (c *Copilot) Logout()        { c.tok = stored{}; os.Remove(c.path()) }
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
	var r tokenReply
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
	c.deviceCode = ""
	return true, c.save(r)
}

type tokenReply struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

func (c *Copilot) save(r tokenReply) error {
	c.tok = stored{Token: r.AccessToken, Refresh: r.RefreshToken}
	if r.ExpiresIn > 0 {
		c.tok.Expires = time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)
	}
	return provider.SaveJSON(c.path(), c.tok)
}

// refresh trades the refresh token for a new access token. Without one (GitHub's usual
// non-expiring token) a refused token can only be fixed by logging in again.
func (c *Copilot) refresh(ctx context.Context) error {
	if c.tok.Refresh == "" {
		return provider.ErrReauth
	}
	var r tokenReply
	err := postForm(ctx, tokenURL, url.Values{
		"client_id":     {clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.tok.Refresh},
	}, &r)
	switch {
	case provider.IsStatus(err, 400, 401, 403):
		return fmt.Errorf("%w: %v", provider.ErrReauth, err)
	case err != nil:
		return err
	case r.Error != "" || r.AccessToken == "": // GitHub reports a bad refresh token with 200
		return fmt.Errorf("%w: refresh: %s %s", provider.ErrReauth, r.Error, r.Description)
	}
	return c.save(r)
}

func (c *Copilot) get(ctx context.Context) ([]byte, error) {
	return provider.Do(ctx, "GET", usageURL, nil, map[string]string{
		"Authorization":  "token " + c.tok.Token,
		"Editor-Version": "kindle-ai-tracker/0.3",
	})
}

func (c *Copilot) Fetch(ctx context.Context) (*provider.Snapshot, []byte, error) {
	if !c.LoggedIn() {
		return nil, nil, provider.ErrReauth
	}
	if c.tok.Refresh != "" && !c.tok.Expires.IsZero() && time.Until(c.tok.Expires) < 5*time.Minute {
		if err := c.refresh(ctx); err != nil {
			return nil, nil, err
		}
	}
	raw, err := c.get(ctx)
	if provider.IsStatus(err, 401) {
		if rerr := c.refresh(ctx); rerr != nil {
			if errors.Is(rerr, provider.ErrReauth) {
				return nil, raw, fmt.Errorf("%w: %v", provider.ErrReauth, err)
			}
			return nil, raw, rerr
		}
		raw, err = c.get(ctx)
		if provider.IsStatus(err, 401) { // refused even with a fresh token
			return nil, raw, fmt.Errorf("%w: %v", provider.ErrReauth, err)
		}
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
		SKU       string           `json:"access_type_sku"`
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
	snap := &provider.Snapshot{Plan: planName(p.Plan, p.SKU)}
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

// planName turns GitHub's internal plan ("individual", "individual_pro") into the name
// GitHub shows (Pro, Pro+). Free accounts are "individual" too; their SKU tells them apart.
func planName(plan, sku string) string {
	switch {
	case strings.Contains(sku, "free"):
		return "Free"
	case plan == "individual_pro" || strings.Contains(sku, "plus"):
		return "Pro+"
	case plan == "individual":
		return "Pro"
	}
	return provider.Capitalize(strings.ReplaceAll(plan, "_", " "))
}

func parseDate(s string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
