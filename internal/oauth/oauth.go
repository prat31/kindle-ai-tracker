// Package oauth has the pieces of an OAuth 2 PKCE login that any provider can reuse:
// verifier/challenge generation, token requests and a token file.
package oauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/prat31/kindle-ai-tracker/internal/provider"
)

var Client = &http.Client{Timeout: 30 * time.Second}

const UserAgent = "kindle-ai-tracker/0.2"

func RandomURLSafe(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// PKCE is one login attempt.
type PKCE struct {
	Verifier, Challenge, State string
}

func NewPKCE() *PKCE {
	p := &PKCE{Verifier: RandomURLSafe(32), State: RandomURLSafe(32)}
	sum := sha256.Sum256([]byte(p.Verifier))
	p.Challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return p
}

// ParseCodeInput accepts "CODE#STATE" (what copy-the-code callback pages show),
// a bare code, or a full redirect URL.
func ParseCodeInput(in string) (code, state string) {
	in = strings.TrimSpace(in)
	if u, err := url.Parse(in); err == nil && u.Query().Get("code") != "" {
		return u.Query().Get("code"), u.Query().Get("state")
	}
	code, state, _ = strings.Cut(in, "#")
	return strings.TrimSpace(code), strings.TrimSpace(state)
}

type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	Scope        string `json:"scope,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
}

func (t *Tokens) ExpiresWithin(d time.Duration) bool {
	return time.Now().Add(d).Unix() > t.ExpiresAt
}

func LoadTokens(path string) *Tokens {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var t Tokens
	if json.Unmarshal(b, &t) != nil || t.AccessToken == "" {
		return nil
	}
	return &t
}

func SaveTokens(path string, t *Tokens) error {
	b, _ := json.MarshalIndent(t, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RequestTokens POSTs a JSON body to a token endpoint. oldRefresh is kept when the
// server doesn't rotate the refresh token.
func RequestTokens(ctx context.Context, tokenURL string, body map[string]string, oldRefresh string) (*Tokens, error) {
	b, _ := json.Marshal(body)
	return requestTokens(ctx, tokenURL, "application/json", b, oldRefresh)
}

// RequestTokensForm is RequestTokens for endpoints that want a form-encoded body.
func RequestTokensForm(ctx context.Context, tokenURL string, body url.Values, oldRefresh string) (*Tokens, error) {
	return requestTokens(ctx, tokenURL, "application/x-www-form-urlencoded", []byte(body.Encode()), oldRefresh)
}

func requestTokens(ctx context.Context, tokenURL, contentType string, body []byte, oldRefresh string) (*Tokens, error) {
	req, _ := http.NewRequestWithContext(ctx, "POST", tokenURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return nil, &provider.HTTPError{Status: resp.StatusCode, Body: string(raw)}
	}
	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil || tr.AccessToken == "" {
		return nil, fmt.Errorf("unexpected token response")
	}
	t := &Tokens{AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken, IDToken: tr.IDToken, Scope: tr.Scope}
	switch {
	case tr.ExpiresIn > 0:
		t.ExpiresAt = time.Now().Unix() + tr.ExpiresIn
	default:
		if exp, ok := JWTClaims(tr.AccessToken)["exp"].(float64); ok {
			t.ExpiresAt = int64(exp)
		} else {
			t.ExpiresAt = time.Now().Add(time.Hour).Unix()
		}
	}
	if t.RefreshToken == "" {
		t.RefreshToken = oldRefresh
	}
	return t, nil
}

// JWTClaims decodes a JWT's payload without verifying it (the token came straight
// from the issuer over TLS); an empty map if it isn't a JWT.
func JWTClaims(token string) map[string]any {
	claims := map[string]any{}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err == nil {
		json.Unmarshal(b, &claims)
	}
	return claims
}
