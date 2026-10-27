// Package openrouter reads OpenRouter credit usage through its documented API:
// GET /api/v1/key (the key's own cap and spend) and GET /api/v1/credits (account balance).
package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prat31/kindle-ai-tracker/internal/provider"
)

const api = "https://openrouter.ai/api/v1"

func init() { provider.Register("openrouter", New) }

// Options: optional spending budgets in USD, shown as meters when set.
type Options struct {
	DailyBudget   float64 `json:"daily_budget_usd"`
	WeeklyBudget  float64 `json:"weekly_budget_usd"`
	MonthlyBudget float64 `json:"monthly_budget_usd"`
}

type OpenRouter struct {
	dir  string
	opts Options
	key  string
}

func New(dir string, raw json.RawMessage) (provider.Provider, error) {
	o := &OpenRouter{dir: dir}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &o.opts); err != nil {
			return nil, fmt.Errorf("openrouter options: %w", err)
		}
	}
	var k provider.APIKey
	if provider.LoadJSON(o.path(), &k) {
		o.key = k.Key
	}
	return o, nil
}

func (o *OpenRouter) ID() string     { return "openrouter" }
func (o *OpenRouter) Name() string   { return "OpenRouter" }
func (o *OpenRouter) LoggedIn() bool { return o.key != "" }
func (o *OpenRouter) path() string   { return filepath.Join(o.dir, "key.json") }
func (o *OpenRouter) Logout()        { o.key = ""; os.Remove(o.path()) }

func (o *OpenRouter) BeginLogin(context.Context) (provider.LoginPrompt, error) {
	return provider.LoginPrompt{
		SignInURL:  "https://openrouter.ai/settings/keys",
		SignInHint: "Create an API key (a credit limit on it helps)",
		InputLabel: "your OpenRouter API key",
	}, nil
}

func (o *OpenRouter) CompleteLogin(ctx context.Context, input string) error {
	key := strings.TrimSpace(input)
	if _, err := get(ctx, key, "/key"); err != nil {
		if provider.IsStatus(err, 401, 403) {
			return errors.New("OpenRouter didn't accept that key")
		}
		return err
	}
	o.key = key
	return provider.SaveJSON(o.path(), provider.APIKey{Key: key})
}

func get(ctx context.Context, key, path string) ([]byte, error) {
	return provider.Do(ctx, "GET", api+path, nil, map[string]string{"Authorization": "Bearer " + key})
}

func (o *OpenRouter) Fetch(ctx context.Context) (*provider.Snapshot, []byte, error) {
	if !o.LoggedIn() {
		return nil, nil, provider.ErrReauth
	}
	keyRaw, err := get(ctx, o.key, "/key")
	if provider.IsStatus(err, 401) {
		return nil, keyRaw, fmt.Errorf("%w: %v", provider.ErrReauth, err)
	}
	if err != nil {
		return nil, keyRaw, err
	}
	credRaw, _ := get(ctx, o.key, "/credits") // the account balance is optional
	snap, err := Parse(keyRaw, credRaw, o.opts, time.Now())
	if err != nil {
		return nil, keyRaw, err
	}
	snap.Fetched = time.Now()
	return snap, keyRaw, nil
}

func money(v float64) string { return fmt.Sprintf("$%.2f", v) }

func Parse(keyRaw, credRaw []byte, opts Options, now time.Time) (*provider.Snapshot, error) {
	var k struct {
		Data struct {
			Limit          *float64 `json:"limit"`
			LimitRemaining *float64 `json:"limit_remaining"`
			LimitReset     *string  `json:"limit_reset"`
			UsageDaily     float64  `json:"usage_daily"`
			UsageWeekly    float64  `json:"usage_weekly"`
			UsageMonthly   float64  `json:"usage_monthly"`
			IsFreeTier     bool     `json:"is_free_tier"`
		} `json:"data"`
	}
	if err := json.Unmarshal(keyRaw, &k); err != nil {
		return nil, fmt.Errorf("key response is not JSON: %w", err)
	}
	d := k.Data
	snap := &provider.Snapshot{Plan: "Paid"}
	if d.IsFreeTier {
		snap.Plan = "Free"
	}

	if d.Limit != nil && *d.Limit > 0 && d.LimitRemaining != nil {
		used := *d.Limit - *d.LimitRemaining
		m := provider.Meter{ID: "key_limit", Title: "Key credit limit", Short: "Key limit",
			Used: provider.Pct(used, *d.Limit), Detail: money(used) + " of " + money(*d.Limit)}
		if d.LimitReset != nil {
			m.ResetsAt, m.Window = provider.NextPeriod(*d.LimitReset, now)
			m.Title = "Key credit limit · " + *d.LimitReset
		}
		snap.Meters = append(snap.Meters, m)
	}
	for _, b := range []struct {
		id, period, title string
		budget, spent     float64
	}{
		{"budget_daily", "daily", "Daily budget", opts.DailyBudget, d.UsageDaily},
		{"budget_weekly", "weekly", "Weekly budget", opts.WeeklyBudget, d.UsageWeekly},
		{"budget_monthly", "monthly", "Monthly budget", opts.MonthlyBudget, d.UsageMonthly},
	} {
		if b.budget <= 0 {
			continue
		}
		reset, window := provider.NextPeriod(b.period, now)
		snap.Meters = append(snap.Meters, provider.Meter{ID: b.id, Title: b.title, Short: b.title,
			Used: provider.Pct(b.spent, b.budget), Window: window, ResetsAt: reset,
			Detail: money(b.spent) + " of " + money(b.budget)})
	}
	var c struct {
		Data struct {
			TotalCredits float64 `json:"total_credits"`
			TotalUsage   float64 `json:"total_usage"`
		} `json:"data"`
	}
	if json.Unmarshal(credRaw, &c) == nil && c.Data.TotalCredits > 0 {
		snap.Meters = append(snap.Meters, provider.Meter{ID: "credits", Title: "Account credits used", Short: "Credits",
			Used:   provider.Pct(c.Data.TotalUsage, c.Data.TotalCredits),
			Detail: money(c.Data.TotalCredits-c.Data.TotalUsage) + " left of " + money(c.Data.TotalCredits)})
	}
	if len(snap.Meters) == 0 {
		return nil, errors.New("nothing to measure: give the key a credit limit, buy credits, or set a budget in config.json")
	}
	return snap, nil
}
