package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Limit is one usage window of a plan, such as Claude's 5-hour session.
type Limit struct {
	ID       string        `json:"id"`   // stable key for history, e.g. "claude/five_hour"
	Name     string        `json:"name"` // shown in the UI, e.g. "Session"
	Used     float64       `json:"used"` // percent of the window used, 0–100
	Window   time.Duration `json:"-"`
	ResetsAt time.Time     `json:"resets_at"` // zero when the window hasn't started
}

// MarshalJSON writes the window in seconds, which is friendlier to scripts
// than Go's nanoseconds.
func (l Limit) MarshalJSON() ([]byte, error) {
	type plain Limit
	return json.Marshal(struct {
		plain
		WindowSeconds int64 `json:"window_seconds"`
	}{plain(l), int64(l.Window.Seconds())})
}

func (l *Limit) UnmarshalJSON(b []byte) error {
	type plain Limit
	var v struct {
		plain
		WindowSeconds int64 `json:"window_seconds"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*l = Limit(v.plain)
	l.Window = time.Duration(v.WindowSeconds) * time.Second
	return nil
}

// Report is what one provider returned on one fetch.
type Report struct {
	Provider string    `json:"provider"`
	Plan     string    `json:"plan,omitempty"`
	Source   string    `json:"source,omitempty"` // where the numbers came from, if not the live API
	Limits   []Limit   `json:"limits"`
	Err      string    `json:"error,omitempty"`
	Missing  bool      `json:"missing,omitempty"` // not signed in; shown dimmed, not as an error
	Fetched  time.Time `json:"fetched"`
}

type Provider struct {
	Key   string // used by --provider
	Name  string
	Fetch func(ctx context.Context) Report
}

var providers = []Provider{
	{"claude", "Claude", fetchClaude},
	{"codex", "Codex", fetchCodex},
}

// errMissing marks a provider that isn't set up on this machine.
type errMissing struct{ hint string }

func (e errMissing) Error() string { return e.hint }

func failed(name string, err error) Report {
	r := Report{Provider: name, Err: err.Error(), Fetched: time.Now()}
	var m errMissing
	if errors.As(err, &m) {
		r.Missing = true
	}
	return r
}

// selectProviders filters by a comma-separated --provider list.
func selectProviders(list string) ([]Provider, error) {
	if list == "" {
		return providers, nil
	}
	var out []Provider
	for _, k := range strings.Split(list, ",") {
		k = strings.ToLower(strings.TrimSpace(k))
		found := false
		for _, p := range providers {
			if p.Key == k {
				out = append(out, p)
				found = true
			}
		}
		if !found {
			return nil, errors.New("unknown provider " + k + " (choose from claude, codex)")
		}
	}
	return out, nil
}

// fetchAll asks every provider at once, records the results in the history,
// and caches them so frequent callers (the status line) don't hit the APIs
// more than once per maxAge.
func fetchAll(ctx context.Context, ps []Provider, maxAge time.Duration) []Report {
	if demoMode {
		var out []Report
		for _, r := range demoReports(time.Now()) {
			for _, p := range ps {
				if p.Name == r.Provider {
					out = append(out, r)
				}
			}
		}
		return out
	}
	if maxAge > 0 {
		if cached, ok := loadCache(ps, maxAge); ok {
			return cached
		}
	}
	reports := make([]Report, len(ps))
	var wg sync.WaitGroup
	for i, p := range ps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reports[i] = p.Fetch(ctx)
			reports[i].Provider = p.Name
		}()
	}
	wg.Wait()
	_ = saveCache(reports)
	_ = recordHistory(reports)
	return reports
}

// ---------- cache ----------

func dataDir() string {
	if d := os.Getenv("LIMITWATCH_DIR"); d != "" {
		return d
	}
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "limitwatch")
}

func cachePath() string { return filepath.Join(dataDir(), "last.json") }

func loadCache(ps []Provider, maxAge time.Duration) ([]Report, bool) {
	b, err := os.ReadFile(cachePath())
	if err != nil {
		return nil, false
	}
	var all []Report
	if json.Unmarshal(b, &all) != nil {
		return nil, false
	}
	var out []Report
	for _, p := range ps {
		found := false
		for _, r := range all {
			if r.Provider == p.Name && time.Since(r.Fetched) < maxAge {
				out = append(out, r)
				found = true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	return out, true
}

func saveCache(reports []Report) error {
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(reports)
	if err != nil {
		return err
	}
	tmp := cachePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, cachePath())
}

// ---------- HTTP ----------

var httpClient = &http.Client{Timeout: 15 * time.Second}

// statusError turns an HTTP failure into a short message for the UI.
func statusError(code int, signInHint string) error {
	switch {
	case code == http.StatusUnauthorized:
		return errors.New("sign-in expired — " + signInHint)
	case code == http.StatusForbidden:
		return errors.New("the usage API refused the request (HTTP 403) — " + signInHint)
	case code == http.StatusTooManyRequests:
		return errors.New("usage API is rate limiting requests; try again in a minute")
	case code >= 500:
		return errors.New("usage API is having trouble (HTTP " + strconv.Itoa(code) + ")")
	default:
		return errors.New("usage API returned HTTP " + strconv.Itoa(code))
	}
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}
