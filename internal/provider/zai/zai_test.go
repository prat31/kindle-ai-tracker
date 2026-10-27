package zai

import "testing"

func TestParse(t *testing.T) {
	s, err := Parse([]byte(`{"code":200,"success":true,"data":{"level":"pro","limits":[
		{"type":"TIME_LIMIT","unit":5,"percentage":3,"nextResetTime":1795000000000},
		{"type":"TOKENS_LIMIT","unit":6,"percentage":20,"nextResetTime":1794000000000},
		{"type":"TOKENS_LIMIT","unit":3,"percentage":55,"nextResetTime":1793000000000}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Plan != "Pro" || len(s.Meters) != 3 {
		t.Fatalf("%+v", s)
	}
	if s.Meters[0].ID != "session" || s.Meters[0].Used != 55 || s.Meters[1].ID != "weekly" || s.Meters[2].ID != "mcp" {
		t.Errorf("%+v", s.Meters)
	}
}
