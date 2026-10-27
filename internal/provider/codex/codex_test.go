package codex

import "testing"

func TestParse(t *testing.T) {
	s, err := Parse([]byte(`{"plan_type":"plus","rate_limit":{
		"primary_window":{"used_percent":42,"limit_window_seconds":18000,"reset_at":1793000000},
		"secondary_window":{"used_percent":12,"limit_window_seconds":604800,"reset_at":1793500000}},
		"additional_rate_limits":[{"limit_name":"Code review","metered_feature":"code_review",
		"rate_limit":{"primary_window":{"used_percent":5,"limit_window_seconds":604800,"reset_at":1793500000}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Plan != "Plus" || len(s.Meters) != 3 {
		t.Fatalf("%+v", s)
	}
	if m := s.Meters[0]; m.ID != "session" || m.Used != 42 || m.Title != "Current session · 5-hour limit" {
		t.Errorf("session: %+v", m)
	}
	if m := s.Meters[1]; m.ID != "weekly" || m.Title != "Weekly limit · all models" {
		t.Errorf("weekly: %+v", m)
	}
	if m := s.Meters[2]; m.ID != "code_review_0" || m.Title != "Weekly limit · Code review" {
		t.Errorf("extra: %+v", m)
	}
}

func TestAccountID(t *testing.T) {
	// header.{"https://api.openai.com/auth":{"chatgpt_account_id":"acc-1"}}.sig
	tok := "e30.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjLTEifX0.x"
	if id := accountID(tok); id != "acc-1" {
		t.Fatalf("got %q", id)
	}
}
