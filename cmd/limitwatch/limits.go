package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Limit is one thing that can run out: a usage window of a plan (Claude's
// 5-hour session), a prepaid balance (DeepSeek credit), or a month's API
// spend against a budget.
type Limit struct {
	ID       string        `json:"id"`   // stable key for history, e.g. "claude/five_hour"
	Name     string        `json:"name"` // shown in the UI, e.g. "Session"
	Kind     LimitKind     `json:"kind,omitempty"`
	Used     float64       `json:"used"` // percent of the window (or budget) used, 0–100
	Window   time.Duration `json:"-"`
	ResetsAt time.Time     `json:"resets_at"` // zero when the window hasn't started, or never resets

	// Optional amounts, in Unit ("USD", "CNY", "requests", …):
	//   window:  Amount used of Total, when the provider counts (Copilot's 79 of 2,000)
	//   balance: Amount left
	//   spend:   Amount spent this month, of a Total budget (0 = no budget)
	Unit   string  `json:"unit,omitempty"`
	Amount float64 `json:"amount,omitempty"`
	Total  float64 `json:"total,omitempty"`
}

type LimitKind string

const (
	KindWindow  LimitKind = ""
	KindBalance LimitKind = "balance"
	KindSpend   LimitKind = "spend"
)

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
	Running  []string  `json:"running,omitempty"` // this provider's apps that are open now
	Hint     string    `json:"hint,omitempty"`    // the next step when the limits couldn't be read
}

type Provider struct {
	Key    string // used by --provider, `key set` and `budget`
	Name   string
	About  string   // one line for the help and README
	KeyEnv []string // environment variables for its API key; empty if it uses an app's sign-in
	Budget bool     // reports API spend, which can have a monthly budget
	Fetch  func(ctx context.Context, p Provider) Report
}

var providers = []Provider{
	{Key: "claude", Name: "Claude", About: "Claude Pro / Max plan limits, from Claude Code's sign-in", Fetch: fetchClaude},
	{Key: "codex", Name: "Codex", About: "ChatGPT plan limits for Codex, from Codex's sign-in", Fetch: fetchCodex},
	{Key: "copilot", Name: "Copilot", About: "GitHub Copilot monthly quotas, from the GitHub CLI's sign-in", Fetch: fetchCopilot},
	{Key: "gemini", Name: "Gemini", About: "Gemini CLI / Code Assist daily quotas, from Gemini CLI's sign-in", Fetch: fetchGemini},
	{Key: "kimi", Name: "Kimi Code", About: "Kimi Code membership quotas", KeyEnv: []string{"KIMI_CODE_API_KEY"}, Fetch: fetchKimiCode},
	{Key: "zai", Name: "Z.ai", About: "Z.ai / Zhipu GLM Coding Plan quotas", KeyEnv: []string{"ZAI_API_KEY", "ZHIPU_API_KEY"}, Fetch: fetchZai},
	{Key: "deepseek", Name: "DeepSeek", About: "DeepSeek API balance", KeyEnv: []string{"DEEPSEEK_API_KEY"}, Fetch: fetchDeepSeek},
	{Key: "moonshot", Name: "Kimi API", About: "Kimi (Moonshot) API balance", KeyEnv: []string{"MOONSHOT_API_KEY", "KIMI_API_KEY"}, Fetch: fetchMoonshot},
	{Key: "openrouter", Name: "OpenRouter", About: "OpenRouter credits and key limit", KeyEnv: []string{"OPENROUTER_API_KEY"}, Fetch: fetchOpenRouter},
	{Key: "openai-api", Name: "OpenAI API", About: "OpenAI API spend this month (admin key)", KeyEnv: []string{"OPENAI_ADMIN_KEY"}, Budget: true, Fetch: fetchOpenAISpend},
	{Key: "anthropic-api", Name: "Anthropic API", About: "Claude API spend this month (admin key)", KeyEnv: []string{"ANTHROPIC_ADMIN_KEY"}, Budget: true, Fetch: fetchAnthropicSpend},
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
			return nil, errors.New("unknown provider " + k + " (choose from " + providerKeys(nil) + ")")
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
			return annotate(cached)
		}
	}
	reports := make([]Report, len(ps))
	var wg sync.WaitGroup
	for i, p := range ps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reports[i] = p.Fetch(ctx, p)
			reports[i].Provider = p.Name
		}()
	}
	wg.Wait()
	_ = saveCache(reports)
	_ = recordHistory(reports)
	return annotate(reports)
}

// inDashboard words hints as commands typed in the dashboard rather than
// shell commands.
var inDashboard bool

// annotate adds which apps are running and, for a provider that failed, what
// to do about it. It runs after caching, so both are always current.
func annotate(reports []Report) []Report {
	running := detectRunning()
	for i := range reports {
		reports[i].Running = running.labels(reports[i].Provider)
		reports[i].Hint = running.hint(reports[i], inDashboard)
	}
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

// getJSON sends a request with the given headers and decodes a 200 reply
// into out. A body of nil means GET; otherwise it's POSTed as JSON.
func getJSON(ctx context.Context, url string, headers map[string]string, body any, out any) error {
	method, rd := http.MethodGet, io.Reader(nil)
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		method, rd = http.MethodPost, bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "limitwatch/"+version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return errors.New("can't reach " + req.URL.Host + ": " + shortNetErr(err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return statusError(resp.StatusCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.New("unexpected reply from " + req.URL.Host + ": " + err.Error())
	}
	return nil
}

// statusError turns an HTTP failure into a short message for the UI.
func statusError(code int) error {
	switch {
	case code == http.StatusUnauthorized:
		return errors.New("sign-in or API key expired, revoked or wrong (HTTP 401)")
	case code == http.StatusForbidden:
		return errors.New("the usage API refused the sign-in or key (HTTP 403)")
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
