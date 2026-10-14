package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// GitHub Copilot reports its monthly quotas through the endpoint its
// editor extensions read. limitwatch asks the GitHub CLI (`gh api`) to make
// the call, so it never handles the GitHub token itself; GH_TOKEN or
// GITHUB_TOKEN work too, for machines without gh.
var copilotUsageURL = "https://api.github.com/copilot_internal/user"

type copilotQuota struct {
	Entitlement      float64 `json:"entitlement"`
	Remaining        float64 `json:"remaining"`
	PercentRemaining float64 `json:"percent_remaining"`
	Unlimited        bool    `json:"unlimited"`
	HasQuota         *bool   `json:"has_quota"`
}

type copilotUser struct {
	Plan           string                  `json:"copilot_plan"`
	SKU            string                  `json:"access_type_sku"`
	ResetDate      string                  `json:"quota_reset_date"`
	ResetDateUTC   string                  `json:"quota_reset_date_utc"`
	QuotaSnapshots map[string]copilotQuota `json:"quota_snapshots"`
}

func fetchCopilot(ctx context.Context, _ Provider) Report {
	r := Report{Provider: "Copilot", Fetched: time.Now()}
	var body []byte
	var err error
	if tok := firstEnv("GH_TOKEN", "GITHUB_TOKEN"); tok != "" {
		var u json.RawMessage
		err = getJSON(ctx, copilotUsageURL, map[string]string{"Authorization": "token " + tok}, nil, &u)
		body = u
	} else if gh, lookErr := exec.LookPath("gh"); lookErr == nil {
		var out []byte
		out, err = exec.CommandContext(ctx, gh, "api", "copilot_internal/user").Output()
		body = out
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			msg := strings.TrimSpace(string(exitErr.Stderr))
			switch {
			case strings.Contains(msg, "auth login"), strings.Contains(msg, "not logged"):
				return failed("Copilot", errMissing{"the GitHub CLI isn't signed in"})
			case strings.Contains(msg, "404"):
				return failed("Copilot", errMissing{"no Copilot plan on this GitHub account"})
			}
			err = errors.New("gh api failed: " + firstLine(msg))
		}
	} else {
		return failed("Copilot", errMissing{"needs the GitHub CLI (gh) signed in, or GH_TOKEN"})
	}
	if err != nil {
		r.Err = err.Error()
		return r
	}
	r.Plan, r.Limits, err = parseCopilot(body)
	if err != nil {
		r.Err = err.Error()
	}
	return r
}

// copilotQuotas names the quotas worth showing, in display order.
var copilotQuotas = []struct{ key, name, unit string }{
	{"premium_interactions", "Premium", "requests"},
	{"chat", "Chat", "messages"},
	{"completions", "Completions", "completions"},
}

func parseCopilot(body []byte) (string, []Limit, error) {
	var u copilotUser
	if err := json.Unmarshal(body, &u); err != nil {
		return "", nil, errors.New("unexpected reply from the Copilot API: " + err.Error())
	}
	plan := title(u.Plan)
	if strings.Contains(u.SKU, "free") {
		plan = "Free"
	}
	reset := parseTimeAny(u.ResetDateUTC)
	if reset.IsZero() {
		reset = parseTimeAny(u.ResetDate)
	}
	window := 30 * 24 * time.Hour
	if !reset.IsZero() {
		window = reset.Sub(reset.AddDate(0, -1, 0))
	}
	var out []Limit
	for _, q := range copilotQuotas {
		s, ok := u.QuotaSnapshots[q.key]
		// Unlimited quotas can't run out, and a zero entitlement means the
		// plan doesn't include it (premium requests on Copilot Free).
		if !ok || s.Unlimited || s.Entitlement <= 0 {
			continue
		}
		out = append(out, Limit{
			ID: "copilot/" + q.key, Name: q.name, Used: clampPct(100 - s.PercentRemaining),
			Window: window, ResetsAt: reset,
			Unit: q.unit, Amount: max(0, s.Entitlement-s.Remaining), Total: s.Entitlement,
		})
	}
	if len(out) == 0 {
		return plan, nil, errors.New("this Copilot plan has no limited quotas")
	}
	return plan, out, nil
}

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

// parseTimeAny reads RFC 3339 timestamps, plain dates, and Unix seconds or
// milliseconds, which different providers use for reset times.
func parseTimeAny(v any) time.Time {
	switch t := v.(type) {
	case string:
		if t == "" {
			return time.Time{}
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z", "2006-01-02 15:04:05", "2006-01-02"} {
			if p, err := time.Parse(layout, t); err == nil {
				return p
			}
		}
	case float64:
		return unixAny(int64(t))
	case int64:
		return unixAny(t)
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return unixAny(n)
		}
	}
	return time.Time{}
}

func unixAny(n int64) time.Time {
	switch {
	case n <= 0:
		return time.Time{}
	case n > 1e12:
		return time.UnixMilli(n)
	default:
		return time.Unix(n, 0)
	}
}
