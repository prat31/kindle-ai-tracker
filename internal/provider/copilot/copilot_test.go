package copilot

import "testing"

func TestParse(t *testing.T) {
	s, err := Parse([]byte(`{"copilot_plan":"individual","quota_reset_date":"2026-11-01",
		"quota_snapshots":{"chat":{"unlimited":true},"completions":{"unlimited":true},
		"premium_interactions":{"entitlement":300,"remaining":71,"percent_remaining":23.7,"unlimited":false}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Meters) != 1 {
		t.Fatalf("%+v", s.Meters)
	}
	m := s.Meters[0]
	if m.Used < 76 || m.Used > 77 || m.Detail != "71 of 300 left" || m.ResetsAt.IsZero() || m.Window < 28*24*3600e9 {
		t.Errorf("%+v", m)
	}
}

func TestPlanName(t *testing.T) {
	for _, c := range []struct{ plan, sku, want string }{
		{"individual", "free_limited_copilot", "Free"},
		{"individual", "monthly_subscriber_quota", "Pro"},
		{"individual_pro", "", "Pro+"},
		{"business", "", "Business"},
		{"enterprise", "", "Enterprise"},
		{"some_new_plan", "", "Some new plan"},
	} {
		if got := planName(c.plan, c.sku); got != c.want {
			t.Errorf("%s/%s: got %q, want %q", c.plan, c.sku, got, c.want)
		}
	}
}
