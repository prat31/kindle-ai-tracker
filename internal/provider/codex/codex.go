// Package codex reads ChatGPT plan limits for Codex (Plus/Pro/Business): the 5-hour
// and weekly windows, plus any per-feature limits.
//
// It logs in with the device-code flow and public OAuth client of the Codex CLI and
// reads the endpoint behind Codex's /status, as implemented in openai/codex
// (codex-rs/login/src/device_code_auth.rs, backend-client .../rate_limit_resets.rs).
// Device-code login must be enabled in ChatGPT: Settings → Security.
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prat31/kindle-ai-tracker/internal/oauth"
	"github.com/prat31/kindle-ai-tracker/internal/provider"
)

const (
	clientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	issuer      = "https://auth.openai.com"
	verifyURL   = issuer + "/codex/device"
	redirectURL = issuer + "/deviceauth/callback"
	tokenURL    = issuer + "/oauth/token"
	usageURL    = "https://chatgpt.com/backend-api/wham/usage"
)

func init() { provider.Register("codex", New) }

type stored struct {
	oauth.Tokens
	AccountID string `json:"account_id"`
}

type Codex struct {
	dir    string
	tokens *stored
	device *deviceCode
}

type deviceCode struct {
	ID       string `json:"device_auth_id"`
	UserCode string `json:"user_code"`
	Interval json.Number
}

func New(dir string, _ json.RawMessage) (provider.Provider, error) {
	c := &Codex{dir: dir}
	var t stored
	if provider.LoadJSON(c.path(), &t) && t.RefreshToken != "" {
		c.tokens = &t
	}
	return c, nil
}

func (c *Codex) ID() string     { return "codex" }
func (c *Codex) Name() string   { return "ChatGPT" }
func (c *Codex) LoggedIn() bool { return c.tokens != nil }
func (c *Codex) path() string   { return filepath.Join(c.dir, "tokens.json") }
func (c *Codex) Logout()        { c.tokens = nil; os.Remove(c.path()) }
func (c *Codex) save() error    { return provider.SaveJSON(c.path(), c.tokens) }
func (c *Codex) CompleteLogin(context.Context, string) error {
	return errors.New("ChatGPT logs in with a device code, nothing to paste")
}

func (c *Codex) BeginLogin(ctx context.Context) (provider.LoginPrompt, error) {
	body, _ := json.Marshal(map[string]string{"client_id": clientID})
	raw, err := provider.Do(ctx, "POST", issuer+"/api/accounts/deviceauth/usercode", bytes.NewReader(body),
		map[string]string{"Content-Type": "application/json"})
	if provider.IsStatus(err, 404) {
		return provider.LoginPrompt{}, errors.New("device code login is off: turn it on in ChatGPT → Settings → Security")
	}
	if err != nil {
		return provider.LoginPrompt{}, err
	}
	var dc deviceCode
	var alt struct {
		UserCode string `json:"usercode"`
	}
	if err := json.Unmarshal(raw, &dc); err != nil {
		return provider.LoginPrompt{}, fmt.Errorf("unexpected device code response: %w", err)
	}
	if dc.UserCode == "" && json.Unmarshal(raw, &alt) == nil {
		dc.UserCode = alt.UserCode
	}
	if dc.ID == "" || dc.UserCode == "" {
		return provider.LoginPrompt{}, errors.New("unexpected device code response")
	}
	c.device = &dc
	every := 5 * time.Second
	if n, err := dc.Interval.Int64(); err == nil && n > 0 {
		every = time.Duration(n) * time.Second
	}
	return provider.LoginPrompt{
		SignInURL:  verifyURL,
		SignInHint: "Sign in to ChatGPT and enter the code",
		UserCode:   dc.UserCode,
		PollEvery:  every,
	}, nil
}

func (c *Codex) PollLogin(ctx context.Context) (bool, error) {
	if c.device == nil {
		return false, errors.New("login not started")
	}
	body, _ := json.Marshal(map[string]string{"device_auth_id": c.device.ID, "user_code": c.device.UserCode})
	raw, err := provider.Do(ctx, "POST", issuer+"/api/accounts/deviceauth/token", bytes.NewReader(body),
		map[string]string{"Content-Type": "application/json"})
	if provider.IsStatus(err, 403, 404) {
		return false, nil // not approved yet
	}
	if err != nil {
		return false, err
	}
	var code struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeVerifier      string `json:"code_verifier"`
	}
	if json.Unmarshal(raw, &code) != nil || code.AuthorizationCode == "" {
		return false, errors.New("unexpected device token response")
	}
	t, err := oauth.RequestTokensForm(ctx, tokenURL, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code.AuthorizationCode},
		"redirect_uri":  {redirectURL},
		"client_id":     {clientID},
		"code_verifier": {code.CodeVerifier},
	}, "")
	if err != nil {
		return false, fmt.Errorf("code exchange failed: %w", err)
	}
	c.tokens, c.device = &stored{Tokens: *t, AccountID: accountID(t.IDToken)}, nil
	return true, c.save()
}

// accountID is the ChatGPT workspace the login belongs to, sent as ChatGPT-Account-Id.
func accountID(idToken string) string {
	auth, _ := oauth.JWTClaims(idToken)["https://api.openai.com/auth"].(map[string]any)
	id, _ := auth["chatgpt_account_id"].(string)
	return id
}

func (c *Codex) refresh(ctx context.Context) error {
	t, err := oauth.RequestTokens(ctx, tokenURL, map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     clientID,
		"refresh_token": c.tokens.RefreshToken,
	}, c.tokens.RefreshToken)
	if provider.IsStatus(err, 400, 401, 403) {
		return fmt.Errorf("%w: %v", provider.ErrReauth, err)
	}
	if err != nil {
		return err
	}
	acc := c.tokens.AccountID
	if id := accountID(t.IDToken); id != "" {
		acc = id
	}
	c.tokens = &stored{Tokens: *t, AccountID: acc}
	return c.save()
}

func (c *Codex) Fetch(ctx context.Context) (*provider.Snapshot, []byte, error) {
	if !c.LoggedIn() {
		return nil, nil, provider.ErrReauth
	}
	if c.tokens.ExpiresWithin(5 * time.Minute) {
		if err := c.refresh(ctx); err != nil {
			return nil, nil, err
		}
	}
	raw, err := c.get(ctx)
	if provider.IsStatus(err, 401) {
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
	snap.Fetched = time.Now()
	return snap, raw, nil
}

func (c *Codex) get(ctx context.Context) ([]byte, error) {
	h := map[string]string{"Authorization": "Bearer " + c.tokens.AccessToken}
	if c.tokens.AccountID != "" {
		h["ChatGPT-Account-Id"] = c.tokens.AccountID
	}
	return provider.Do(ctx, "GET", usageURL, nil, h)
}

type window struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

type rateLimit struct {
	Primary   *window `json:"primary_window"`
	Secondary *window `json:"secondary_window"`
}

func Parse(raw []byte) (*provider.Snapshot, error) {
	var p struct {
		PlanType   string     `json:"plan_type"`
		RateLimit  *rateLimit `json:"rate_limit"`
		Additional []struct {
			LimitName      string     `json:"limit_name"`
			MeteredFeature string     `json:"metered_feature"`
			RateLimit      *rateLimit `json:"rate_limit"`
		} `json:"additional_rate_limits"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("usage response is not JSON: %w", err)
	}
	snap := &provider.Snapshot{Plan: provider.Capitalize(p.PlanType)}
	if p.RateLimit != nil {
		if m := meter("session", "Session", p.RateLimit.Primary, func(w string) string { return "Current session · " + w }); m != nil {
			snap.Meters = append(snap.Meters, *m)
		}
		if m := meter("weekly", "Weekly", p.RateLimit.Secondary, func(w string) string { return w + " · all models" }); m != nil {
			snap.Meters = append(snap.Meters, *m)
		}
	}
	for _, a := range p.Additional {
		if a.RateLimit == nil {
			continue
		}
		name := a.LimitName
		if name == "" {
			name = a.MeteredFeature
		}
		id := strings.ToLower(strings.NewReplacer(" ", "_", "-", "_").Replace(name))
		for i, w := range []*window{a.RateLimit.Primary, a.RateLimit.Secondary} {
			if m := meter(fmt.Sprintf("%s_%d", id, i), name, w, func(w string) string { return w + " · " + name }); m != nil {
				snap.Meters = append(snap.Meters, *m)
			}
		}
	}
	if len(snap.Meters) == 0 {
		return nil, errors.New("usage response has no limits we recognise")
	}
	return snap, nil
}

// meter turns one window into a meter; title gets the window's name, e.g. "5-hour limit".
func meter(id, short string, w *window, title func(string) string) *provider.Meter {
	if w == nil {
		return nil
	}
	d := time.Duration(w.LimitWindowSeconds) * time.Second
	m := &provider.Meter{ID: id, Title: title(provider.WindowTitle(d)), Short: short, Used: w.UsedPercent, Window: d}
	if w.ResetAt > 0 {
		m.ResetsAt = time.Unix(w.ResetAt, 0)
	}
	return m
}
