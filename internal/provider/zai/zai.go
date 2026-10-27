// Package zai reads the Z.ai (Zhipu) GLM Coding Plan quotas: the 5-hour and weekly
// token windows and the monthly MCP tool allowance. It uses the endpoint behind
// Z.ai's own usage plugin, which isn't documented publicly.
package zai

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

func init() { provider.Register("zai", New) }

// Options: "region": "cn" for open.bigmodel.cn accounts.
type Options struct {
	Region string `json:"region"`
}

type ZAI struct {
	dir  string
	opts Options
	key  string
}

func New(dir string, raw json.RawMessage) (provider.Provider, error) {
	z := &ZAI{dir: dir}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &z.opts); err != nil {
			return nil, fmt.Errorf("zai options: %w", err)
		}
	}
	var k provider.APIKey
	if provider.LoadJSON(z.path(), &k) {
		z.key = k.Key
	}
	return z, nil
}

func (z *ZAI) ID() string     { return "zai" }
func (z *ZAI) Name() string   { return "Z.ai GLM" }
func (z *ZAI) LoggedIn() bool { return z.key != "" }
func (z *ZAI) path() string   { return filepath.Join(z.dir, "key.json") }
func (z *ZAI) Logout()        { z.key = ""; os.Remove(z.path()) }

func (z *ZAI) base() string {
	if z.opts.Region == "cn" {
		return "https://open.bigmodel.cn"
	}
	return "https://api.z.ai"
}

func (z *ZAI) BeginLogin(context.Context) (provider.LoginPrompt, error) {
	page := "https://z.ai/manage-apikey/apikey-list"
	if z.opts.Region == "cn" {
		page = "https://open.bigmodel.cn/usercenter/apikeys"
	}
	return provider.LoginPrompt{SignInURL: page, SignInHint: "Copy your Coding Plan API key",
		InputLabel: "your Z.ai API key"}, nil
}

func (z *ZAI) CompleteLogin(ctx context.Context, input string) error {
	key := strings.TrimSpace(input)
	raw, err := z.get(ctx, key)
	if err != nil {
		if provider.IsStatus(err, 401, 403) {
			return errors.New("Z.ai didn't accept that key")
		}
		return err
	}
	if _, err := Parse(raw); err != nil {
		return err
	}
	z.key = key
	return provider.SaveJSON(z.path(), provider.APIKey{Key: key})
}

// get sends the key as-is, which is what Z.ai's plugin does, and retries as a Bearer
// token if that's refused.
func (z *ZAI) get(ctx context.Context, key string) ([]byte, error) {
	u := z.base() + "/api/monitor/usage/quota/limit"
	raw, err := provider.Do(ctx, "GET", u, nil, map[string]string{"Authorization": key})
	if provider.IsStatus(err, 401, 403) {
		raw, err = provider.Do(ctx, "GET", u, nil, map[string]string{"Authorization": "Bearer " + key})
	}
	return raw, err
}

func (z *ZAI) Fetch(ctx context.Context) (*provider.Snapshot, []byte, error) {
	if !z.LoggedIn() {
		return nil, nil, provider.ErrReauth
	}
	raw, err := z.get(ctx, z.key)
	if provider.IsStatus(err, 401, 403) {
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

func Parse(raw []byte) (*provider.Snapshot, error) {
	var env struct {
		Success bool   `json:"success"`
		Msg     string `json:"msg"`
		Data    *struct {
			Level  string `json:"level"`
			Limits []struct {
				Type          string  `json:"type"`
				Percentage    float64 `json:"percentage"`
				NextResetTime int64   `json:"nextResetTime"`
				Unit          int     `json:"unit"`
			} `json:"limits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("quota response is not JSON: %w", err)
	}
	if env.Data == nil {
		if env.Msg != "" {
			return nil, errors.New("Z.ai: " + env.Msg)
		}
		return nil, errors.New("Z.ai returned no quota data (is this a Coding Plan key?)")
	}
	snap := &provider.Snapshot{Plan: provider.Capitalize(env.Data.Level)}
	tokenLimits := 0
	for _, l := range env.Data.Limits {
		m := provider.Meter{Used: l.Percentage}
		if l.NextResetTime > 0 {
			m.ResetsAt = time.UnixMilli(l.NextResetTime)
		}
		switch {
		case l.Type == "TOKENS_LIMIT":
			// unit 3 is the 5-hour window and 6 the weekly one; fall back to order.
			weekly := l.Unit == 6 || (l.Unit != 3 && tokenLimits > 0)
			tokenLimits++
			if weekly {
				m.ID, m.Title, m.Short, m.Window = "weekly", "Weekly limit · tokens", "Weekly", 7*24*time.Hour
			} else {
				m.ID, m.Title, m.Short, m.Window = "session", "Current session · 5-hour limit", "Session", 5*time.Hour
			}
		case l.Type == "TIME_LIMIT":
			m.ID, m.Title, m.Short, m.Window = "mcp", "MCP tool calls · monthly", "MCP", 30*24*time.Hour
		default:
			continue
		}
		snap.Meters = append(snap.Meters, m)
	}
	// Session first, then weekly, then the rest.
	order := map[string]int{"session": 0, "weekly": 1, "mcp": 2}
	for i := 1; i < len(snap.Meters); i++ {
		for j := i; j > 0 && order[snap.Meters[j].ID] < order[snap.Meters[j-1].ID]; j-- {
			snap.Meters[j], snap.Meters[j-1] = snap.Meters[j-1], snap.Meters[j]
		}
	}
	if len(snap.Meters) == 0 {
		return nil, errors.New("Z.ai returned no quota limits")
	}
	return snap, nil
}
