package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prat31/kindle-ai-tracker/internal/provider"
)

func TestSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	reset := time.Date(2026, 10, 28, 18, 0, 0, 0, time.UTC)
	snap := &provider.Snapshot{
		Plan:    "Pro",
		Fetched: time.Date(2026, 10, 28, 12, 30, 0, 0, time.UTC),
		Meters: []provider.Meter{
			{ID: "session", Title: "Current session", Short: "Session", Used: 42.5,
				Window: 5 * time.Hour, ResetsAt: reset, Detail: "$1.20 of $10"},
			{ID: "weekly", Title: "Weekly limit", Short: "Week", Used: 12},
		},
	}
	saveSnapshot(dir, snap)

	got := loadSnapshot(dir)
	if got == nil {
		t.Fatal("loadSnapshot returned nil after save")
	}
	if got.Plan != "Pro" || !got.Fetched.Equal(snap.Fetched) || len(got.Meters) != 2 {
		t.Fatalf("got %+v, want %+v", got, snap)
	}
	m := got.Meters[0]
	if m.Used != 42.5 || m.Window != 5*time.Hour || !m.ResetsAt.Equal(reset) || m.Detail != "$1.20 of $10" {
		t.Fatalf("meter did not survive the round trip: %+v", m)
	}

	// The temporary file is renamed into place, not left behind.
	if _, err := os.Stat(snapshotPath(dir) + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind: %v", err)
	}
}

func TestLoadSnapshotMissingOrBroken(t *testing.T) {
	dir := t.TempDir()
	if got := loadSnapshot(dir); got != nil {
		t.Fatalf("missing file: got %+v, want nil", got)
	}
	os.WriteFile(filepath.Join(dir, snapshotFile), []byte("{not json"), 0o644)
	if got := loadSnapshot(dir); got != nil {
		t.Fatalf("broken file: got %+v, want nil", got)
	}
	// A snapshot without meters would draw an empty dashboard.
	os.WriteFile(filepath.Join(dir, snapshotFile), []byte(`{"Fetched":"2026-10-28T12:30:00Z"}`), 0o644)
	if got := loadSnapshot(dir); got != nil {
		t.Fatalf("empty snapshot: got %+v, want nil", got)
	}
}

func TestClearSnapshot(t *testing.T) {
	dir := t.TempDir()
	saveSnapshot(dir, &provider.Snapshot{Fetched: time.Now(), Meters: []provider.Meter{{ID: "session", Used: 1}}})
	clearSnapshot(dir)
	if got := loadSnapshot(dir); got != nil {
		t.Fatalf("got %+v after clear, want nil", got)
	}
}
