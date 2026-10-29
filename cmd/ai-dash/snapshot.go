package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/prat31/kindle-ai-tracker/internal/provider"
)

// snapshotFile keeps the last successful reading in the provider's data directory, so
// starting the dashboard without a connection still shows the numbers it last saw.
const snapshotFile = "last_snapshot.json"

func snapshotPath(dir string) string { return filepath.Join(dir, snapshotFile) }

// loadSnapshot returns the reading saved by saveSnapshot, or nil if there is none.
func loadSnapshot(dir string) *provider.Snapshot {
	b, err := os.ReadFile(snapshotPath(dir))
	if err != nil {
		return nil
	}
	var snap provider.Snapshot
	if json.Unmarshal(b, &snap) != nil || snap.Fetched.IsZero() || len(snap.Meters) == 0 {
		return nil
	}
	return &snap
}

// saveSnapshot records a successful reading for the next start.
func saveSnapshot(dir string, snap *provider.Snapshot) {
	b, err := json.Marshal(snap)
	if err != nil {
		return
	}
	path := snapshotPath(dir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		log.Printf("saving last reading: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("saving last reading: %v", err)
	}
}

// clearSnapshot forgets the last reading, e.g. when the connector is removed.
func clearSnapshot(dir string) { os.Remove(snapshotPath(dir)) }
