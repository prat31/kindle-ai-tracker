package claude

import (
	"testing"

	"github.com/prat31/kindle-ai-tracker/internal/oauth"
)

func TestParseLegacy(t *testing.T) {
	s, err := Parse([]byte(`{"five_hour":{"utilization":42.0,"resets_at":"2026-10-28T03:00:00.123+00:00"},
		"seven_day":{"utilization":35,"resets_at":"2026-10-31T05:30:00Z"},"seven_day_opus":null,
		"seven_day_sonnet":{"utilization":12,"resets_at":"2026-10-31T05:30:00Z"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Meters) != 3 {
		t.Fatalf("meters: %+v", s.Meters)
	}
	if m := s.Meter("session"); m == nil || m.Used != 42 || m.ResetsAt.IsZero() {
		t.Fatalf("session: %+v", m)
	}
	if m := s.Meter("weekly"); m == nil || m.Used != 35 {
		t.Fatalf("weekly: %+v", m)
	}
	if m := s.Meter("weekly_sonnet"); m == nil || m.Short != "Sonnet" {
		t.Fatalf("sonnet: %+v", s.Meters)
	}
}

func TestParseLimits(t *testing.T) {
	s, err := Parse([]byte(`{"limits":[
		{"kind":"session","group":"session","percent":7,"resets_at":"2026-10-28T03:00:00Z"},
		{"kind":"weekly_all","group":"weekly","percent":51,"resets_at":"2026-10-31T05:30:00Z"},
		{"kind":"weekly_scoped","group":"weekly","percent":20,"resets_at":null,"scope":{"model":{"display_name":"Opus"}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Meters[0].ID != "session" || s.Meters[0].Used != 7 || s.Meters[1].Used != 51 || s.Meters[2].Short != "Opus" {
		t.Fatalf("meters: %+v", s.Meters)
	}
}

func TestParseCodeInput(t *testing.T) {
	for in, want := range map[string][2]string{
		" abc#xyz \n": {"abc", "xyz"},
		"abc":         {"abc", ""},
		"https://platform.claude.com/oauth/code/callback?code=abc&state=xyz": {"abc", "xyz"},
	} {
		c, s := oauth.ParseCodeInput(in)
		if c != want[0] || s != want[1] {
			t.Errorf("%q -> %q %q", in, c, s)
		}
	}
}

func TestPlanName(t *testing.T) {
	for raw, want := range map[string]string{
		`{"account":{"has_claude_pro":true},"organization":{"organization_type":"claude_pro","rate_limit_tier":"default_claude_pro"}}`:     "Pro",
		`{"account":{"has_claude_max":true},"organization":{"organization_type":"claude_max","rate_limit_tier":"default_claude_max_5x"}}`:  "Max 5x",
		`{"account":{"has_claude_max":true},"organization":{"organization_type":"claude_max","rate_limit_tier":"default_claude_max_20x"}}`: "Max 20x",
		`{"account":{"has_claude_max":true},"organization":{}}`:                                                                            "Max",
		`{"organization":{"organization_type":"claude_team","rate_limit_tier":"default_claude_max_5x"}}`:                                   "Team",
		`{"organization":{"organization_type":"claude_enterprise"}}`:                                                                       "Enterprise",
		`{"account":{},"organization":{}}`: "",
		`not json`:                         "",
	} {
		if got := PlanName([]byte(raw)); got != want {
			t.Errorf("%s: got %q, want %q", raw, got, want)
		}
	}
}
