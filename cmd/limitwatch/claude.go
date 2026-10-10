package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Claude plans report usage through the same endpoint Claude Code's /usage
// screen reads, authorized with the OAuth token Claude Code keeps locally.
// limitwatch only reads that token; it never refreshes or rewrites it.
var claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"

type claudeCreds struct {
	AccessToken      string `json:"accessToken"`
	ExpiresAt        int64  `json:"expiresAt"` // Unix milliseconds
	SubscriptionType string `json:"subscriptionType"`
	RateLimitTier    string `json:"rateLimitTier"`
}

func claudeConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(homeDir(), ".claude")
}

func loadClaudeCreds() (claudeCreds, error) {
	if tok := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); tok != "" {
		return claudeCreds{AccessToken: tok}, nil
	}
	raw, err := os.ReadFile(filepath.Join(claudeConfigDir(), ".credentials.json"))
	if errors.Is(err, fs.ErrNotExist) && runtime.GOOS == "darwin" {
		// On macOS, Claude Code keeps its credentials in the login keychain.
		var out []byte
		out, err = exec.Command("security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
		raw = out
	}
	if err != nil {
		return claudeCreds{}, errMissing{"not signed in to Claude Code"}
	}
	var f struct {
		OAuth claudeCreds `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &f); err != nil || f.OAuth.AccessToken == "" {
		return claudeCreds{}, errMissing{"no Claude subscription sign-in found (API-key logins have no plan limits)"}
	}
	return f.OAuth, nil
}

func fetchClaude(ctx context.Context) Report {
	creds, err := loadClaudeCreds()
	if err != nil {
		return failed("Claude", err)
	}
	r := Report{Provider: "Claude", Plan: claudePlan(creds), Fetched: time.Now()}
	if creds.ExpiresAt > 0 && time.UnixMilli(creds.ExpiresAt).Before(time.Now()) {
		r.Err = "Claude Code's saved sign-in expired " + humanAge(time.UnixMilli(creds.ExpiresAt), time.Now())
		return r
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, claudeUsageURL, nil)
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "limitwatch/"+version)
	resp, err := httpClient.Do(req)
	if err != nil {
		r.Err = "can't reach the Claude usage API: " + shortNetErr(err)
		return r
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		r.Err = statusError(resp.StatusCode).Error()
		return r
	}
	r.Limits, err = parseClaudeUsage(body)
	if err != nil {
		r.Err = err.Error()
	}
	return r
}

// claudeWindows names the windows we know about; anything else the API adds
// later is still shown, with a name built from its key.
var claudeWindows = map[string]struct {
	name   string
	window time.Duration
	order  int
}{
	"five_hour":            {"Session", 5 * time.Hour, 0},
	"seven_day":            {"Weekly", 7 * 24 * time.Hour, 1},
	"seven_day_opus":       {"Weekly Opus", 7 * 24 * time.Hour, 2},
	"seven_day_sonnet":     {"Weekly Sonnet", 7 * 24 * time.Hour, 3},
	"seven_day_oauth_apps": {"Weekly apps", 7 * 24 * time.Hour, 4},
}

func parseClaudeUsage(body []byte) ([]Limit, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unexpected reply from the Claude usage API: %w", err)
	}
	type win struct {
		Utilization *float64 `json:"utilization"`
		ResetsAt    *string  `json:"resets_at"`
	}
	var out []Limit
	for key, v := range raw {
		var w win
		if json.Unmarshal(v, &w) != nil || w.Utilization == nil {
			continue // not a usage window (null, or something like extra_usage)
		}
		l := Limit{ID: "claude/" + key, Used: clampPct(*w.Utilization)}
		if info, ok := claudeWindows[key]; ok {
			l.Name, l.Window = info.name, info.window
		} else {
			l.Name, l.Window = keyName(key), guessWindow(key)
		}
		if w.ResetsAt != nil && *w.ResetsAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, *w.ResetsAt); err == nil {
				l.ResetsAt = t
			}
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil, errors.New("the Claude usage API returned no limits")
	}
	sort.Slice(out, func(i, j int) bool {
		oi, oj := orderOf(out[i].ID), orderOf(out[j].ID)
		if oi != oj {
			return oi < oj
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func orderOf(id string) int {
	if info, ok := claudeWindows[strings.TrimPrefix(id, "claude/")]; ok {
		return info.order
	}
	return 99
}

func claudePlan(c claudeCreds) string {
	plan := title(c.SubscriptionType)
	for _, mult := range []string{"20x", "5x"} {
		if strings.Contains(c.RateLimitTier, mult) {
			plan += " " + mult
			break
		}
	}
	return plan
}

// keyName turns "seven_day_foo" into "Seven day foo".
func keyName(key string) string { return title(strings.ReplaceAll(key, "_", " ")) }

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func guessWindow(key string) time.Duration {
	switch {
	case strings.HasPrefix(key, "five_hour"):
		return 5 * time.Hour
	case strings.HasPrefix(key, "seven_day"):
		return 7 * 24 * time.Hour
	case strings.HasPrefix(key, "thirty_day"), strings.HasPrefix(key, "monthly"):
		return 30 * 24 * time.Hour
	}
	return 0
}

func clampPct(v float64) float64 { return max(0, min(100, v)) }

// shortNetErr keeps the useful tail of a network error ("no such host",
// "connection refused", "timeout") rather than the whole URL chain.
func shortNetErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return s
}
