package opencode

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s, err := Parse([]byte(`{"usage":{
		"rolling":{"status":"ok","percent":42.5,"resetsAt":"2026-10-28T14:30:00Z"},
		"weekly":{"status":"ok","percent":18,"resetsAt":1793318400},
		"monthly":{"status":"ok","percent":7,"resetsAt":1795000000000}}}`), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Meters) != 3 {
		t.Fatalf("%+v", s)
	}
	m := s.Meters
	if m[0].ID != "session" || m[0].Used != 42.5 || !m[0].ResetsAt.Equal(time.Date(2026, 10, 28, 14, 30, 0, 0, time.UTC)) {
		t.Errorf("session: %+v", m[0])
	}
	if m[1].ID != "weekly" || !m[1].ResetsAt.Equal(time.Unix(1793318400, 0)) {
		t.Errorf("weekly: %+v", m[1])
	}
	if m[2].ID != "monthly" || !m[2].ResetsAt.Equal(time.UnixMilli(1795000000000)) {
		t.Errorf("monthly: %+v", m[2])
	}
}

// The console's own status endpoint uses usagePercent and resetInSec, unwrapped.
func TestParseConsoleShape(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s, err := Parse([]byte(`{"rolling":{"usagePercent":60,"resetInSec":3600},"weekly":{"usagePercent":20,"resetInSec":86400}}`), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Meters) != 2 || s.Meters[0].Used != 60 || !s.Meters[0].ResetsAt.Equal(now.Add(time.Hour)) {
		t.Errorf("%+v", s.Meters)
	}
}

func TestParseEmpty(t *testing.T) {
	if _, err := Parse([]byte(`{"type":"error","error":{"type":"AuthError","message":"Unauthorized"}}`), time.Now()); err == nil {
		t.Error("want an error for a reply with no limits")
	}
}
