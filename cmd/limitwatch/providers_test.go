package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseClaudeUsage(t *testing.T) {
	body := `{
		"five_hour": {"utilization": 42.5, "resets_at": "2026-10-10T15:00:00.123+00:00"},
		"seven_day": {"utilization": 12, "resets_at": "2026-10-14T09:00:00Z"},
		"seven_day_opus": {"utilization": 3, "resets_at": null},
		"seven_day_oauth_apps": null,
		"thirty_day_new_thing": {"utilization": 7, "resets_at": null},
		"extra_usage": {"is_enabled": false}
	}`
	ls, err := parseClaudeUsage([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, l := range ls {
		names = append(names, l.Name)
	}
	if got := strings.Join(names, ","); got != "Session,Weekly,Weekly Opus,Thirty day new thing" {
		t.Fatalf("names %s", got)
	}
	s := ls[0]
	if s.Used != 42.5 || s.Window != 5*time.Hour || !s.ResetsAt.Equal(time.Date(2026, 10, 10, 15, 0, 0, 123e6, time.UTC)) {
		t.Fatalf("session %+v", s)
	}
	if !ls[2].ResetsAt.IsZero() || ls[3].Window != 30*24*time.Hour {
		t.Fatalf("opus/new %+v %+v", ls[2], ls[3])
	}
	if _, err := parseClaudeUsage([]byte(`{"error":"nope"}`)); err == nil {
		t.Fatal("expected an error for a reply without limits")
	}
}

func TestClaudePlan(t *testing.T) {
	for _, c := range []struct{ sub, tier, want string }{
		{"max", "default_claude_max_20x", "Max 20x"},
		{"max", "default_claude_max_5x", "Max 5x"},
		{"pro", "", "Pro"},
		{"", "", ""},
	} {
		if got := claudePlan(claudeCreds{SubscriptionType: c.sub, RateLimitTier: c.tier}); got != c.want {
			t.Errorf("claudePlan(%q, %q) = %q, want %q", c.sub, c.tier, got, c.want)
		}
	}
}

func TestFetchClaude(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	creds := `{"claudeAiOauth":{"accessToken":"tok-123","expiresAt":` +
		strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10) + `,"subscriptionType":"pro"}}`
	os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(creds), 0o600)

	var gotAuth, gotBeta string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotBeta = r.Header.Get("Authorization"), r.Header.Get("anthropic-beta")
		w.WriteHeader(status)
		w.Write([]byte(`{"five_hour":{"utilization":10,"resets_at":"2026-10-10T15:00:00Z"}}`))
	}))
	defer srv.Close()
	old := claudeUsageURL
	claudeUsageURL = srv.URL
	defer func() { claudeUsageURL = old }()

	r := fetchClaude(context.Background(), providers[0])
	if r.Err != "" || len(r.Limits) != 1 || r.Plan != "Pro" {
		t.Fatalf("report %+v", r)
	}
	if gotAuth != "Bearer tok-123" || gotBeta == "" {
		t.Fatalf("headers %q %q", gotAuth, gotBeta)
	}

	status = http.StatusUnauthorized
	if r := fetchClaude(context.Background(), providers[0]); !strings.Contains(r.Err, "HTTP 401") {
		t.Fatalf("401 → %q", r.Err)
	}

	os.Remove(filepath.Join(dir, ".credentials.json"))
	if r := fetchClaude(context.Background(), providers[0]); !r.Missing {
		t.Fatalf("no credentials → %+v", r)
	}
}

func TestParseCodexUsage(t *testing.T) {
	now := time.Unix(1_760_000_000, 0)
	body := `{"plan_type":"plus","rate_limit":{"allowed":true,"limit_reached":false,
		"primary_window":{"used_percent":23,"limit_window_seconds":18000,"reset_after_seconds":3600,"reset_at":1760003600},
		"secondary_window":{"used_percent":5,"limit_window_seconds":604800,"reset_after_seconds":86400}}}`
	plan, ls, err := parseCodexUsage([]byte(body), now)
	if err != nil {
		t.Fatal(err)
	}
	if plan != "Plus" || len(ls) != 2 {
		t.Fatalf("plan %q, %d limits", plan, len(ls))
	}
	if ls[0].Name != "5-hour" || ls[0].Used != 23 || !ls[0].ResetsAt.Equal(time.Unix(1_760_003_600, 0)) {
		t.Fatalf("primary %+v", ls[0])
	}
	if ls[1].Name != "Weekly" || !ls[1].ResetsAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("secondary %+v", ls[1])
	}
}

func TestCodexFromLogs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	now := time.Now()
	dir := filepath.Join(home, "sessions", now.Format("2006/01/02"))
	os.MkdirAll(dir, 0o755)
	ts := now.Add(-10 * time.Minute).UTC().Format(time.RFC3339Nano)
	log := `{"timestamp":"` + ts + `","type":"session_meta","payload":{}}
{"timestamp":"` + ts + `","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":40,"window_minutes":300,"resets_in_seconds":3600},"secondary":{"used_percent":9,"window_minutes":10080,"resets_in_seconds":60}}}}
`
	os.WriteFile(filepath.Join(dir, "rollout-2026-a.jsonl"), []byte(log), 0o644)

	r, at, ok := codexFromLogs(now)
	if !ok || len(r.Limits) != 2 {
		t.Fatalf("ok=%v %+v", ok, r)
	}
	if p := r.Limits[0]; p.Used != 40 || p.Window != 5*time.Hour || p.ResetsAt.Sub(now).Round(time.Minute) != 50*time.Minute {
		t.Fatalf("primary %+v", p)
	}
	// The weekly window reset 9 minutes ago, so it starts over at 0.
	if s := r.Limits[1]; s.Used != 0 || !s.ResetsAt.IsZero() {
		t.Fatalf("secondary %+v", s)
	}
	if now.Sub(at).Round(time.Minute) != 10*time.Minute {
		t.Fatalf("reading time %v", at)
	}
	if !strings.Contains(r.Source, "Codex's log, 10m ago") {
		t.Fatalf("source %q", r.Source)
	}
}

func TestCacheAndHistory(t *testing.T) {
	t.Setenv("LIMITWATCH_DIR", t.TempDir())
	t.Setenv("LIMITWATCH_HISTORY", "")
	now := time.Now()
	reports := []Report{{Provider: "Claude", Fetched: now, Limits: []Limit{session(10, time.Hour)}}}
	if err := saveCache(reports); err != nil {
		t.Fatal(err)
	}
	got, ok := loadCache(providers[:1], time.Minute)
	if !ok || got[0].Limits[0].Window != 5*time.Hour || got[0].Limits[0].Used != 10 {
		t.Fatalf("cache %v %+v", ok, got)
	}
	if _, ok := loadCache(providers, time.Minute); ok {
		t.Fatal("cache hit although Codex was never fetched")
	}

	if err := recordHistory(reports); err != nil {
		t.Fatal(err)
	}
	recordHistory(reports)
	if h := loadHistory(); len(h["claude/five_hour"]) != 2 {
		t.Fatalf("history %+v", h)
	}
}
