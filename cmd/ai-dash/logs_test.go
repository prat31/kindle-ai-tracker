package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneLog(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.Local)
	path := filepath.Join(t.TempDir(), "dash.log")
	in := "=== start Mon Oct 26\n" +
		"2026/10/26 09:00:00 old\n" +
		"ai-dash exited rc=0\n" +
		"=== start Tue Oct 27\n" +
		"2026/10/27 12:30:00 recent\n" +
		"2026/10/28 11:59:00 newest\n"
	os.WriteFile(path, []byte(in), 0o644)

	if err := pruneLog(path, logKeep, now); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "2026/10/27 12:30:00 recent\n2026/10/28 11:59:00 newest\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// Nothing old: left alone.
	if err := pruneLog(path, logKeep, now); err != nil {
		t.Fatal(err)
	}
	if got2, _ := os.ReadFile(path); string(got2) != want {
		t.Fatalf("second prune changed the log: %q", got2)
	}
}
