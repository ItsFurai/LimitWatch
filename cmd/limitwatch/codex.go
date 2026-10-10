package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ChatGPT plans (Codex) report usage through the endpoint Codex's /status
// reads, authorized with the sign-in Codex keeps in ~/.codex/auth.json.
// When that fails, the rate-limit snapshot Codex writes into its session
// logs after every message is used instead.
var codexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// codexLogFresh is how old a log reading can be and still count as current.
const codexLogFresh = 15 * time.Minute

func codexHome() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	return filepath.Join(homeDir(), ".codex")
}

type codexAuth struct {
	Tokens *struct {
		AccessToken string `json:"access_token"`
		AccountID   string `json:"account_id"`
	} `json:"tokens"`
}

func fetchCodex(ctx context.Context) Report {
	r := Report{Provider: "Codex", Fetched: time.Now()}
	var auth codexAuth
	b, err := os.ReadFile(filepath.Join(codexHome(), "auth.json"))
	if err == nil {
		err = json.Unmarshal(b, &auth)
	}
	if err != nil || auth.Tokens == nil || auth.Tokens.AccessToken == "" {
		// API-key logins have no plan limits, but session logs may still have some.
		return codexFallback(errMissing{"not signed in to Codex with ChatGPT"})
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, codexUsageURL, nil)
	req.Header.Set("Authorization", "Bearer "+auth.Tokens.AccessToken)
	if auth.Tokens.AccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", auth.Tokens.AccountID)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "limitwatch/"+version)
	resp, err := httpClient.Do(req)
	if err != nil {
		err = errors.New("can't reach the ChatGPT usage API: " + shortNetErr(err))
	} else {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode != http.StatusOK {
			err = statusError(resp.StatusCode)
		} else {
			r.Plan, r.Limits, err = parseCodexUsage(body, time.Now())
		}
	}
	if err != nil {
		return codexFallback(err)
	}
	return r
}

// codexFallback uses the newest reading from Codex's logs after the live
// check failed with err. A recent reading is as good as a live one, so the
// error is only shown when the reading is getting old.
func codexFallback(err error) Report {
	now := time.Now()
	r, at, ok := codexFromLogs(now)
	if !ok {
		return failed("Codex", err)
	}
	if now.Sub(at) > codexLogFresh {
		r.Err = "live check failed (" + err.Error() + "), so these numbers may be out of date"
	}
	return r
}

type codexWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

func parseCodexUsage(body []byte, now time.Time) (string, []Limit, error) {
	var u struct {
		PlanType  string `json:"plan_type"`
		RateLimit *struct {
			Primary   *codexWindow `json:"primary_window"`
			Secondary *codexWindow `json:"secondary_window"`
		} `json:"rate_limit"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return "", nil, fmt.Errorf("unexpected reply from the ChatGPT usage API: %w", err)
	}
	if u.RateLimit == nil || (u.RateLimit.Primary == nil && u.RateLimit.Secondary == nil) {
		return title(u.PlanType), nil, errors.New("the ChatGPT usage API returned no limits")
	}
	var out []Limit
	for i, w := range []*codexWindow{u.RateLimit.Primary, u.RateLimit.Secondary} {
		if w == nil {
			continue
		}
		l := codexLimit(i, w.UsedPercent, time.Duration(w.LimitWindowSeconds)*time.Second)
		switch {
		case w.ResetAt > 0:
			l.ResetsAt = time.Unix(w.ResetAt, 0)
		case w.ResetAfterSeconds > 0:
			l.ResetsAt = now.Add(time.Duration(w.ResetAfterSeconds) * time.Second)
		}
		out = append(out, l)
	}
	return title(u.PlanType), out, nil
}

func codexLimit(slot int, used float64, window time.Duration) Limit {
	id := []string{"codex/primary", "codex/secondary"}[slot]
	return Limit{ID: id, Name: windowName(window, slot), Used: clampPct(used), Window: window}
}

func windowName(w time.Duration, slot int) string {
	switch {
	case w == 0:
		return []string{"Short window", "Long window"}[slot]
	case w <= 6*time.Hour:
		return fmt.Sprintf("%d-hour", int(w.Round(time.Hour).Hours()))
	case w >= 6*24*time.Hour && w <= 8*24*time.Hour:
		return "Weekly"
	case w >= 28*24*time.Hour:
		return "Monthly"
	default:
		return humanDur(w)
	}
}

// codexFromLogs reads the newest rate-limit snapshot from Codex's session
// logs (sessions/YYYY/MM/DD/rollout-*.jsonl). It's only as fresh as your
// last Codex message, so the report says how old it is.
func codexFromLogs(now time.Time) (Report, time.Time, bool) {
	files := recentRollouts(filepath.Join(codexHome(), "sessions"), now, 8)
	for _, f := range files {
		ts, rl, ok := lastRateLimits(f)
		if !ok {
			continue
		}
		r := Report{Provider: "Codex", Fetched: now, Source: "from Codex's log, " + humanAge(ts, now)}
		for i, w := range []*codexLogWindow{rl.Primary, rl.Secondary} {
			if w == nil {
				continue
			}
			l := codexLimit(i, w.UsedPercent, time.Duration(w.WindowMinutes)*time.Minute)
			switch {
			case w.ResetsAt > 0:
				l.ResetsAt = time.Unix(w.ResetsAt, 0)
			case w.ResetsInSeconds > 0:
				l.ResetsAt = ts.Add(time.Duration(w.ResetsInSeconds) * time.Second)
			}
			if !l.ResetsAt.IsZero() && l.ResetsAt.Before(now) {
				l.Used, l.ResetsAt = 0, time.Time{} // that window has reset since
			}
			r.Limits = append(r.Limits, l)
		}
		if len(r.Limits) > 0 {
			return r, ts, true
		}
	}
	return Report{}, time.Time{}, false
}

type codexLogWindow struct {
	UsedPercent     float64 `json:"used_percent"`
	WindowMinutes   int64   `json:"window_minutes"`
	ResetsInSeconds int64   `json:"resets_in_seconds"`
	ResetsAt        int64   `json:"resets_at"`
}

type codexLogLimits struct {
	Primary   *codexLogWindow `json:"primary"`
	Secondary *codexLogWindow `json:"secondary"`
}

// recentRollouts lists rollout files from the last `days` day folders, newest first.
func recentRollouts(root string, now time.Time, days int) []string {
	type file struct {
		path string
		mod  time.Time
	}
	var found []file
	for d := 0; d < days; d++ {
		dir := filepath.Join(root, now.AddDate(0, 0, -d).Format("2006/01/02"))
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), "rollout-") || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			if info, err := e.Info(); err == nil {
				found = append(found, file{filepath.Join(dir, e.Name()), info.ModTime()})
			}
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].mod.After(found[j].mod) })
	out := make([]string, len(found))
	for i, f := range found {
		out[i] = f.path
	}
	return out
}

func lastRateLimits(path string) (time.Time, codexLogLimits, bool) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, codexLogLimits{}, false
	}
	defer f.Close()
	var (
		bestT  time.Time
		best   codexLogLimits
		found  bool
		needle = []byte(`"rate_limits"`)
	)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, needle) {
			continue
		}
		var ev struct {
			Timestamp time.Time `json:"timestamp"`
			Payload   struct {
				RateLimits *codexLogLimits `json:"rate_limits"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Payload.RateLimits == nil {
			continue
		}
		if ev.Payload.RateLimits.Primary == nil && ev.Payload.RateLimits.Secondary == nil {
			continue
		}
		bestT, best, found = ev.Timestamp, *ev.Payload.RateLimits, true
	}
	return bestT, best, found
}
