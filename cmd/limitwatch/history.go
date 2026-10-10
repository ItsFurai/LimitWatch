package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Sample is one recorded reading of a limit. The history is a small JSON
// Lines file; the forecast reads it to measure your recent pace.
type Sample struct {
	T     int64   `json:"t"` // Unix seconds
	ID    string  `json:"id"`
	Used  float64 `json:"used"`
	Reset int64   `json:"reset,omitempty"` // Unix seconds; identifies the window
}

func (s Sample) Time() time.Time { return time.Unix(s.T, 0) }

const (
	historyKeep    = 8 * 24 * time.Hour // a little more than the longest window we forecast
	historyMaxSize = 2 << 20            // compact the file when it grows past this
)

func historyPath() string {
	if p := os.Getenv("LIMITWATCH_HISTORY"); p != "" {
		return p
	}
	return filepath.Join(dataDir(), "history.jsonl")
}

// History maps a limit ID to its samples, oldest first.
type History map[string][]Sample

func loadHistory() History {
	h := History{}
	f, err := os.Open(historyPath())
	if err != nil {
		return h
	}
	defer f.Close()
	cutoff := time.Now().Add(-historyKeep).Unix()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var s Sample
		if json.Unmarshal(sc.Bytes(), &s) != nil || s.T < cutoff {
			continue
		}
		h[s.ID] = append(h[s.ID], s)
	}
	return h
}

func recordHistory(reports []Report) error {
	var lines []byte
	for _, r := range reports {
		for _, l := range r.Limits {
			s := Sample{T: r.Fetched.Unix(), ID: l.ID, Used: l.Used}
			if !l.ResetsAt.IsZero() {
				s.Reset = l.ResetsAt.Unix()
			}
			b, _ := json.Marshal(s)
			lines = append(append(lines, b...), '\n')
		}
	}
	if len(lines) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(historyPath()), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(historyPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(lines)
	f.Close()
	if st, e := os.Stat(historyPath()); e == nil && st.Size() > historyMaxSize {
		compactHistory()
	}
	return err
}

// compactHistory rewrites the file without samples older than historyKeep.
func compactHistory() {
	h := loadHistory()
	tmp := historyPath() + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	w := bufio.NewWriter(f)
	for _, samples := range h {
		for _, s := range samples {
			b, _ := json.Marshal(s)
			w.Write(append(b, '\n'))
		}
	}
	if w.Flush() != nil || f.Close() != nil {
		os.Remove(tmp)
		return
	}
	os.Rename(tmp, historyPath())
}

// inWindow returns the samples that belong to the same window as l: same
// reset time (give or take a few minutes, as providers round it).
func (h History) inWindow(l Limit) []Sample {
	if l.ResetsAt.IsZero() {
		return nil
	}
	start := l.ResetsAt.Add(-l.Window).Unix()
	var out []Sample
	for _, s := range h[l.ID] {
		d := s.Reset - l.ResetsAt.Unix()
		if s.Reset != 0 && d > -900 && d < 900 && (l.Window == 0 || s.T >= start) {
			out = append(out, s)
		}
	}
	return out
}
