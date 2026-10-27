// Package provider defines what an AI service plugs into the dashboard.
//
// A provider knows how to log in and how to turn the service's usage data into
// Meters: "x% of some limit used, resetting at T". Everything else (history,
// trends, drawing, the Kindle) is generic and works for any provider.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Meter is one usage limit, e.g. Claude's 5-hour session or weekly cap.
type Meter struct {
	ID       string        // stable key used for history, e.g. "session"
	Title    string        // panel heading, e.g. "Current session · 5-hour limit"
	Short    string        // compact name for charts, e.g. "Session"
	Used     float64       // percent of the limit used, 0-100
	Window   time.Duration // length of the limit window; 0 if unknown
	ResetsAt time.Time     // zero if unknown
	Detail   string        // optional absolute numbers, e.g. "$3.20 of $10" or "71 of 300 left"
}

// Snapshot is one reading from a provider.
type Snapshot struct {
	Plan    string  // e.g. "Pro"
	Meters  []Meter // most important first: the first two get big panels
	Fetched time.Time
}

func (s *Snapshot) Meter(id string) *Meter {
	for i := range s.Meters {
		if s.Meters[i].ID == id {
			return &s.Meters[i]
		}
	}
	return nil
}

// LoginPrompt tells the Kindle what to show while logging in. There are two kinds:
//
//   - paste: the user opens SignInURL (optional) and pastes a code or key on the phone
//     page; the app then calls CompleteLogin.
//   - device code: UserCode is set; the user enters it at SignInURL and the app calls
//     PollLogin every PollEvery until it reports done.
type LoginPrompt struct {
	SignInURL  string // shown as a QR code; empty when there's nothing to open (e.g. API keys)
	SignInHint string // what to do on that page
	InputLabel string // paste logins: what the user pastes, e.g. "your API key"
	UserCode   string // device-code logins: the code to type at SignInURL
	PollEvery  time.Duration
}

// ErrReauth means the stored login no longer works and the user must log in again.
var ErrReauth = errors.New("login expired")

type Provider interface {
	ID() string
	Name() string // shown in the header, e.g. "Claude"
	LoggedIn() bool
	// BeginLogin starts a fresh login attempt; CompleteLogin finishes a paste login with
	// what the user pasted.
	BeginLogin(ctx context.Context) (LoginPrompt, error)
	CompleteLogin(ctx context.Context, input string) error
	Logout()
	// Fetch returns the current usage. raw is the service's reply, saved for debugging.
	// Return an error wrapping ErrReauth when the user has to log in again.
	Fetch(ctx context.Context) (snap *Snapshot, raw []byte, err error)
}

// Poller is implemented by providers with device-code logins.
type Poller interface {
	// PollLogin reports whether the user has approved the device code yet. An error
	// means this attempt is over (expired or denied) and a new one should begin.
	PollLogin(ctx context.Context) (done bool, err error)
}

// Factory builds a provider. dir is a private directory for its tokens and state;
// opts is the provider's entry from config.json.
type Factory func(dir string, opts json.RawMessage) (Provider, error)

var registry = map[string]Factory{}

// Register is called from a provider package's init().
func Register(id string, f Factory) { registry[id] = f }

func New(id, dir string, opts json.RawMessage) (Provider, error) {
	f, ok := registry[id]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q (available: %v)", id, IDs())
	}
	return f(dir, opts)
}

func IDs() []string {
	var ids []string
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	body := e.Body
	if len(body) > 200 {
		body = body[:200]
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, body)
}

// LoadJSON and SaveJSON keep a provider's secrets in its data directory (0600).
func LoadJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(b, v) == nil
}

func SaveJSON(path string, v any) error {
	b, _ := json.MarshalIndent(v, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Pct is used/total as a percentage, 0 when total is 0.
func Pct(used, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return used / total * 100
}

// WindowTitle names a limit window by its length, e.g. "5-hour limit", "Weekly limit".
func WindowTitle(d time.Duration) string {
	switch h := d.Hours(); {
	case d <= 0:
		return "Limit"
	case h == 24*7:
		return "Weekly limit"
	case h == 24:
		return "Daily limit"
	case h >= 24*28 && h <= 24*31:
		return "Monthly limit"
	case h >= 48 && int(h)%24 == 0:
		return fmt.Sprintf("%d-day limit", int(h)/24)
	default:
		return fmt.Sprintf("%g-hour limit", h)
	}
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

const userAgent = "kindle-ai-tracker/0.3"

// Do sends a request with the given headers and returns the body; a non-2xx status
// becomes an *HTTPError (the body is still returned, for debugging).
func Do(ctx context.Context, method, url string, body io.Reader, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return raw, &HTTPError{Status: resp.StatusCode, Body: string(raw)}
	}
	return raw, nil
}

// IsStatus reports whether err is an *HTTPError with one of the given statuses.
func IsStatus(err error, statuses ...int) bool {
	var he *HTTPError
	if !errors.As(err, &he) {
		return false
	}
	for _, s := range statuses {
		if he.Status == s {
			return true
		}
	}
	return false
}

// Capitalize upper-cases the first letter: "plus" → "Plus".
func Capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// APIKey is the stored secret of providers that log in with a pasted key.
type APIKey struct {
	Key string `json:"api_key"`
}

// NextPeriod is when a UTC calendar period ("daily", "weekly" from Monday, "monthly")
// next starts, and how long the period is; zero values for anything else.
func NextPeriod(period string, now time.Time) (time.Time, time.Duration) {
	now = now.UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch period {
	case "daily", "day":
		return day.AddDate(0, 0, 1), 24 * time.Hour
	case "weekly", "week":
		next := day.AddDate(0, 0, (8-int(day.Weekday()))%7)
		if !next.After(now) {
			next = next.AddDate(0, 0, 7)
		}
		return next, 7 * 24 * time.Hour
	case "monthly", "month":
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		next := start.AddDate(0, 1, 0)
		return next, next.Sub(start)
	}
	return time.Time{}, 0
}
