package openrouter

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC) // a Wednesday
	s, err := Parse([]byte(`{"data":{"limit":10,"limit_remaining":7.5,"limit_reset":"weekly","usage_monthly":4}}`),
		[]byte(`{"data":{"total_credits":20,"total_usage":5}}`), Options{MonthlyBudget: 8}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Meters) != 3 {
		t.Fatalf("%+v", s.Meters)
	}
	key := s.Meters[0]
	if key.Used != 25 || key.Detail != "$2.50 of $10.00" || !key.ResetsAt.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("key: %+v", key)
	}
	if b := s.Meters[1]; b.Used != 50 || !b.ResetsAt.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("budget: %+v", b)
	}
	if c := s.Meters[2]; c.Used != 25 || c.Detail != "$15.00 left of $20.00" {
		t.Errorf("credits: %+v", c)
	}
}

func TestParseNothing(t *testing.T) {
	if _, err := Parse([]byte(`{"data":{"limit":null}}`), nil, Options{}, time.Now()); err == nil {
		t.Fatal("expected an error with nothing to measure")
	}
}
