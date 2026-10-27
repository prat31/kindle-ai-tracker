// Package history keeps every reading of every meter in a JSON-lines file and
// derives trends from it. It knows nothing about specific providers.
package history

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"time"

	"github.com/prat31/kindle-ai-tracker/internal/provider"
)

const Keep = 15 * 24 * time.Hour

type Point struct {
	U float64 `json:"u"`           // percent used
	R int64   `json:"r,omitempty"` // window reset (unix); a change means a new window
}

type Sample struct {
	T int64            `json:"t"`
	M map[string]Point `json:"m"`
}

type Store struct {
	path    string
	Samples []Sample
}

func Open(path string) *Store {
	s := &Store{path: path}
	f, err := os.Open(path)
	if err != nil {
		return s
	}
	defer f.Close()
	cutoff := time.Now().Add(-Keep).Unix()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var smp Sample
		if json.Unmarshal(sc.Bytes(), &smp) == nil && smp.T >= cutoff && smp.M != nil {
			s.Samples = append(s.Samples, smp)
		}
	}
	return s
}

func FromSnapshot(snap *provider.Snapshot) Sample {
	smp := Sample{T: snap.Fetched.Unix(), M: map[string]Point{}}
	for _, m := range snap.Meters {
		p := Point{U: m.Used}
		if !m.ResetsAt.IsZero() {
			p.R = m.ResetsAt.Unix()
		}
		smp.M[m.ID] = p
	}
	return smp
}

func (s *Store) Add(smp Sample) {
	s.Samples = append(s.Samples, smp)
	cutoff := time.Now().Add(-Keep).Unix()
	trimmed := 0
	for trimmed < len(s.Samples) && s.Samples[trimmed].T < cutoff {
		trimmed++
	}
	if trimmed > 0 || s.path == "" {
		s.Samples = s.Samples[trimmed:]
		s.rewrite()
		return
	}
	if f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		b, _ := json.Marshal(smp)
		f.Write(append(b, '\n'))
		f.Close()
	}
}

func (s *Store) rewrite() {
	if s.path == "" {
		return
	}
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	w := bufio.NewWriter(f)
	for _, smp := range s.Samples {
		b, _ := json.Marshal(smp)
		w.Write(append(b, '\n'))
	}
	w.Flush()
	f.Close()
	os.Rename(tmp, s.path)
}

// TimePoint is one reading of a single meter.
type TimePoint struct {
	T time.Time
	U float64
	R int64
}

// Series returns the readings of one meter since a point in time.
func (s *Store) Series(id string, since time.Time) []TimePoint {
	var out []TimePoint
	for _, smp := range s.Samples {
		if smp.T < since.Unix() {
			continue
		}
		if p, ok := smp.M[id]; ok {
			out = append(out, TimePoint{time.Unix(smp.T, 0), p.U, p.R})
		}
	}
	return out
}

// Rate is %/hour for the meter's current window, preferring the last hour of readings
// (usage is bursty) and falling back to the average since the window began.
func (s *Store) Rate(m provider.Meter, now time.Time) (float64, bool) {
	reset := int64(0)
	if !m.ResetsAt.IsZero() {
		reset = m.ResetsAt.Unix()
	}
	for _, p := range s.Series(m.ID, now.Add(-time.Hour)) {
		if p.R != reset {
			continue
		}
		if hrs := now.Sub(p.T).Hours(); hrs >= 0.25 {
			return math.Max(0, (m.Used-p.U)/hrs), true
		}
		break
	}
	if !m.ResetsAt.IsZero() && m.Window > 0 && m.ResetsAt.After(now) {
		elapsed := (m.Window - m.ResetsAt.Sub(now)).Hours()
		if elapsed >= 0.25 {
			return m.Used / elapsed, true
		}
	}
	return 0, false
}

type DayValue struct {
	Day   time.Time
	Value float64
}

func DayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func lastDays(now time.Time, loc *time.Location, n int) []DayValue {
	today := DayStart(now.In(loc))
	out := make([]DayValue, n)
	for i := range out {
		out[i].Day = today.AddDate(0, 0, i-n+1)
	}
	return out
}

func dayIndex(days []DayValue, t time.Time, loc *time.Location) int {
	return int(math.Round(DayStart(t.In(loc)).Sub(days[0].Day).Hours() / 24))
}

// UsedPerDay is how many points of a meter's limit were consumed on each of the last n days.
func (s *Store) UsedPerDay(id string, now time.Time, loc *time.Location, n int) []DayValue {
	days := lastDays(now, loc, n)
	pts := s.Series(id, days[0].Day.Add(-24*time.Hour))
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		d := b.U - a.U
		if a.R != 0 && b.R != 0 && a.R != b.R && b.U < a.U {
			d = b.U // the window reset between readings
		}
		if idx := dayIndex(days, b.T, loc); d > 0 && idx >= 0 && idx < n {
			days[idx].Value += d
		}
	}
	return days
}

// PeakPerDay is the highest reading of a meter on each of the last n days.
func (s *Store) PeakPerDay(id string, now time.Time, loc *time.Location, n int) []DayValue {
	days := lastDays(now, loc, n)
	for _, p := range s.Series(id, days[0].Day) {
		if idx := dayIndex(days, p.T, loc); idx >= 0 && idx < n {
			days[idx].Value = math.Max(days[idx].Value, p.U)
		}
	}
	return days
}

// ActiveHoursToday counts distinct hours today in which the meter went up.
func (s *Store) ActiveHoursToday(id string, now time.Time, loc *time.Location) int {
	pts := s.Series(id, DayStart(now.In(loc)).Add(-time.Hour))
	hours := map[int]bool{}
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		if b.T.Before(DayStart(now.In(loc))) {
			continue
		}
		if b.U > a.U || (b.R != a.R && b.U > 0) {
			hours[b.T.In(loc).Hour()] = true
		}
	}
	return len(hours)
}
