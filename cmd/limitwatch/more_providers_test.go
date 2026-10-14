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

// serve answers every request with body and records the last request.
func serve(t *testing.T, status int, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	last := &http.Request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*last = *r.Clone(context.Background())
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, last
}

func withVar[T any](t *testing.T, v *T, val T) {
	old := *v
	*v = val
	t.Cleanup(func() { *v = old })
}

func isolate(t *testing.T) {
	t.Setenv("LIMITWATCH_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	for _, p := range providers {
		for _, env := range p.KeyEnv {
			t.Setenv(env, "")
		}
	}
}

func provider(t *testing.T, key string) Provider {
	p, err := findProvider(key)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A real reply from a Copilot Free account, trimmed to the fields that matter.
const copilotReply = `{"login":"someone","access_type_sku":"free_limited_copilot","copilot_plan":"individual",
 "quota_reset_date":"2026-11-01","quota_reset_date_utc":"2026-11-01T00:00:00.000Z",
 "quota_snapshots":{
  "chat":{"entitlement":200,"remaining":200,"percent_remaining":100.0,"unlimited":false,"has_quota":true},
  "completions":{"entitlement":2000,"remaining":1921,"percent_remaining":96.0,"unlimited":false,"has_quota":true},
  "premium_interactions":{"entitlement":0,"remaining":0,"percent_remaining":0.0,"unlimited":false,"has_quota":false}}}`

func TestParseCopilot(t *testing.T) {
	plan, ls, err := parseCopilot([]byte(copilotReply))
	if err != nil {
		t.Fatal(err)
	}
	if plan != "Free" || len(ls) != 2 {
		t.Fatalf("plan %q, %d limits (premium with no entitlement should be skipped)", plan, len(ls))
	}
	c := ls[1]
	if c.Name != "Completions" || c.Used != 4 || c.Amount != 79 || c.Total != 2000 || c.Unit != "completions" {
		t.Fatalf("completions %+v", c)
	}
	if !c.ResetsAt.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) || c.Window != 31*24*time.Hour {
		t.Fatalf("reset %v window %v", c.ResetsAt, c.Window)
	}
	if got := countText(c); got != "79 / 2,000 completions" {
		t.Fatalf("count %q", got)
	}
	pro := strings.Replace(copilotReply, `"entitlement":0,"remaining":0,"percent_remaining":0.0`, `"entitlement":300,"remaining":71,"percent_remaining":23.7`, 1)
	pro = strings.Replace(pro, "free_limited_copilot", "copilot_pro", 1)
	plan, ls, _ = parseCopilot([]byte(pro))
	if plan != "Individual" || ls[0].Name != "Premium" || ls[0].Amount != 229 {
		t.Fatalf("pro: %q %+v", plan, ls[0])
	}
}

func TestFetchCopilotWithToken(t *testing.T) {
	srv, req := serve(t, 200, copilotReply)
	withVar(t, &copilotUsageURL, srv.URL)
	t.Setenv("GH_TOKEN", "gho_test")
	r := fetchCopilot(context.Background(), provider(t, "copilot"))
	if r.Err != "" || len(r.Limits) != 2 || req.Header.Get("Authorization") != "token gho_test" {
		t.Fatalf("%+v, auth %q", r, req.Header.Get("Authorization"))
	}
}

func TestFetchGemini(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GEMINI_CLI_HOME", home)
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	p := provider(t, "gemini")
	if r := fetchGemini(context.Background(), p); !r.Missing {
		t.Fatalf("no creds: %+v", r)
	}
	os.MkdirAll(filepath.Join(home, ".gemini"), 0o700)
	creds := `{"access_token":"ya29.test","expiry_date":` + itoa(time.Now().Add(time.Hour).UnixMilli()) + `}`
	os.WriteFile(filepath.Join(home, ".gemini", "oauth_creds.json"), []byte(creds), 0o600)

	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer ya29.test" {
			w.WriteHeader(401)
			return
		}
		if strings.HasSuffix(r.URL.Path, ":loadCodeAssist") {
			w.Write([]byte(`{"cloudaicompanionProject":"proj-1","currentTier":{"id":"free-tier","name":"Gemini Code Assist for individuals"}}`))
			return
		}
		w.Write([]byte(`{"buckets":[
			{"modelId":"gemini-2.5-flash","tokenType":"REQUESTS","remainingFraction":0.9,"resetTime":"2026-10-15T00:00:00Z"},
			{"modelId":"gemini-2.5-pro","tokenType":"REQUESTS","remainingFraction":0.25,"resetTime":"2026-10-15T00:00:00Z"},
			{"modelId":"gemini-2.5-pro_vertex","tokenType":"REQUESTS","remainingFraction":0.25}]}`))
	}))
	defer srv.Close()
	withVar(t, &geminiAPI, srv.URL+"/v1internal")
	r := fetchGemini(context.Background(), p)
	if r.Err != "" || r.Plan != "Free" || len(r.Limits) != 2 {
		t.Fatalf("%+v (requests: %v)", r, paths)
	}
	if l := r.Limits[0]; l.Name != "2.5 Pro" || l.Used != 75 || l.Window != 24*time.Hour {
		t.Fatalf("pro first: %+v", l)
	}
}

func TestKimiCode(t *testing.T) {
	isolate(t)
	body := `{"user":{"membership":{"level":"LEVEL_INTERMEDIATE"}},
		"usage":{"limit":"2048","used":"512","remaining":"1536","resetTime":"2026-10-20T00:00:00Z"},
		"limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},"detail":{"limit":200,"remaining":150,"resetTime":"2026-10-14T20:00:00Z"}}]}`
	srv, req := serve(t, 200, body)
	withVar(t, &kimiCodeURL, srv.URL)
	p := provider(t, "kimi")
	if r := fetchKimiCode(context.Background(), p); !r.Missing {
		t.Fatalf("no key: %+v", r)
	}
	t.Setenv("KIMI_CODE_API_KEY", "sk-kimi-test")
	r := fetchKimiCode(context.Background(), p)
	if r.Err != "" || r.Plan != "Intermediate" || len(r.Limits) != 2 || req.Header.Get("Authorization") != "Bearer sk-kimi-test" {
		t.Fatalf("%+v", r)
	}
	if s, w := r.Limits[0], r.Limits[1]; s.Name != "5-hour" || s.Used != 25 || w.Name != "Weekly" || w.Used != 25 || w.ResetsAt.IsZero() {
		t.Fatalf("limits %+v %+v", s, w)
	}
}

func TestMoonshotTriesChinaSite(t *testing.T) {
	isolate(t)
	bad, _ := serve(t, 401, `{"error":{"message":"Invalid Authentication"}}`)
	good, _ := serve(t, 200, `{"code":0,"data":{"available_balance":49.58,"voucher_balance":46.58,"cash_balance":3.0},"scode":"0x0","status":true}`)
	withVar(t, &moonshotBaseURL, []string{bad.URL, good.URL + "/.cn"})
	t.Setenv("MOONSHOT_API_KEY", "sk-test")
	r := fetchMoonshot(context.Background(), provider(t, "moonshot"))
	if r.Err != "" || len(r.Limits) != 1 {
		t.Fatalf("%+v", r)
	}
	if l := r.Limits[0]; l.Kind != KindBalance || l.Amount != 49.58 || l.Unit != "CNY" {
		t.Fatalf("balance %+v", l)
	}
}

func TestDeepSeek(t *testing.T) {
	isolate(t)
	srv, req := serve(t, 200, `{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"12.40","granted_balance":"2.00","topped_up_balance":"10.40"}]}`)
	withVar(t, &deepseekURL, srv.URL)
	if err := setKey("deepseek", "  sk-saved-key  "); err != nil {
		t.Fatal(err)
	}
	r := fetchDeepSeek(context.Background(), provider(t, "deepseek"))
	if r.Err != "" || req.Header.Get("Authorization") != "Bearer sk-saved-key" {
		t.Fatalf("%+v auth %q", r, req.Header.Get("Authorization"))
	}
	if l := r.Limits[0]; l.Amount != 12.4 || l.Unit != "USD" || valueText(l) != "$12.40" || r.Plan != "$2.00 of it granted" {
		t.Fatalf("%+v plan %q", l, r.Plan)
	}
	t.Setenv("DEEPSEEK_API_KEY", "sk-env")
	fetchDeepSeek(context.Background(), provider(t, "deepseek"))
	if req.Header.Get("Authorization") != "Bearer sk-env" {
		t.Fatal("the environment variable should win over the saved key")
	}
}

func TestZai(t *testing.T) {
	ls, err := parseZai([]byte(`{"code":200,"msg":"success","success":true,"data":{"limits":[
		{"type":"TOKENS_LIMIT","usage":10000000,"currentValue":500000,"percentage":5,"nextResetTime":1760500000000},
		{"type":"TIME_LIMIT","usage":100,"currentValue":10,"percentage":10}]}}`))
	if err != nil || len(ls) != 2 {
		t.Fatalf("%v %+v", err, ls)
	}
	if ls[0].Name != "5-hour" || ls[0].Used != 5 || !ls[0].ResetsAt.Equal(time.UnixMilli(1760500000000)) {
		t.Fatalf("tokens %+v", ls[0])
	}
	if ls[1].Amount != 10 || ls[1].Total != 100 || countText(ls[1]) != "10 / 100 calls" {
		t.Fatalf("tools %+v", ls[1])
	}
	if _, err := parseZai([]byte(`{"success":false,"msg":"token expired"}`)); err == nil || !strings.Contains(err.Error(), "token expired") {
		t.Fatalf("failure: %v", err)
	}
}

func TestOpenRouter(t *testing.T) {
	isolate(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/credits":
			w.Write([]byte(`{"data":{"total_credits":50,"total_usage":12.5}}`))
		case "/key":
			w.Write([]byte(`{"data":{"label":"sk-or-v1-abc","limit":100,"limit_remaining":74.5,"limit_reset":"monthly","usage":25.5,"is_free_tier":false}}`))
		}
	}))
	defer srv.Close()
	withVar(t, &openRouterURL, srv.URL)
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")
	r := fetchOpenRouter(context.Background(), provider(t, "openrouter"))
	if r.Err != "" || len(r.Limits) != 2 {
		t.Fatalf("%+v", r)
	}
	if c := r.Limits[0]; c.Kind != KindBalance || c.Amount != 37.5 {
		t.Fatalf("credits %+v", c)
	}
	if k := r.Limits[1]; k.Kind != KindWindow || k.Used != 25.5 || k.ResetsAt.Day() != 1 || k.Window < 28*24*time.Hour {
		t.Fatalf("key %+v", k)
	}

	never := "never"
	lim, rem := flexNum(10), flexNum(4)
	if k := openRouterKeyLimit(&lim, &rem, &never, time.Now()); k.Kind != KindBalance || k.Amount != 4 {
		t.Fatalf("non-resetting key limit should be a balance: %+v", k)
	}
	weekly := "weekly"
	monday := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC) // a Monday
	if k := openRouterKeyLimit(&lim, &rem, &weekly, monday); !k.ResetsAt.Equal(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("weekly from a Monday resets next Monday, got %v", k.ResetsAt)
	}
}

func TestSpend(t *testing.T) {
	isolate(t)
	anth, areq := serve(t, 200, `{"data":[{"starting_at":"2026-10-01T00:00:00Z","ending_at":"2026-10-02T00:00:00Z","results":[{"amount":"1250.5","currency":"USD"}]},
		{"starting_at":"2026-10-02T00:00:00Z","ending_at":"2026-10-03T00:00:00Z","results":[{"amount":"749.5","currency":"USD"}]}],"has_more":false,"next_page":null}`)
	withVar(t, &anthropicCostsURL, anth.URL)
	t.Setenv("ANTHROPIC_ADMIN_KEY", "sk-ant-admin-test")
	r := fetchAnthropicSpend(context.Background(), provider(t, "anthropic-api"))
	if r.Err != "" || r.Limits[0].Amount != 20 || r.Limits[0].Total != 0 {
		t.Fatalf("anthropic %+v", r)
	}
	if areq.Header.Get("x-api-key") != "sk-ant-admin-test" || !strings.HasPrefix(areq.URL.Query().Get("starting_at"), time.Now().UTC().Format("2006-01")+"-01T00:00:00") {
		t.Fatalf("anthropic request %v %v", areq.Header, areq.URL.RawQuery)
	}

	oai, oreq := serve(t, 200, `{"object":"page","data":[{"object":"bucket","start_time":1,"end_time":2,"results":[{"amount":{"value":3.5,"currency":"usd"}},{"amount":{"value":1.5,"currency":"usd"}}]}],"has_more":false,"next_page":null}`)
	withVar(t, &openAICostsURL, oai.URL)
	t.Setenv("OPENAI_ADMIN_KEY", "sk-admin-test")
	if err := setBudget("openai-api", "$20"); err != nil {
		t.Fatal(err)
	}
	r = fetchOpenAISpend(context.Background(), provider(t, "openai-api"))
	if l := r.Limits[0]; r.Err != "" || l.Amount != 5 || l.Total != 20 || l.Used != 25 || l.Kind != KindSpend {
		t.Fatalf("openai %+v", r)
	}
	if oreq.Header.Get("Authorization") != "Bearer sk-admin-test" || oreq.URL.Query().Get("bucket_width") != "1d" {
		t.Fatalf("openai request %v", oreq.URL.RawQuery)
	}
	if err := setBudget("deepseek", "10"); err == nil {
		t.Fatal("budgets are only for API spend")
	}
}

func TestPredictBalance(t *testing.T) {
	l := Limit{ID: "deepseek/usd", Kind: KindBalance, Unit: "USD", Amount: 10}
	at := func(ago time.Duration, amt float64) Sample {
		return Sample{T: t0.Add(-ago).Unix(), ID: l.ID, Amount: amt}
	}

	if f := Predict(l, nil, t0); f.Verdict != Idle {
		t.Fatalf("no history: %v", f.Verdict)
	}
	// $2/day for two days, with a $20 top-up in between that must not count as spending.
	hist := []Sample{at(48*time.Hour, 4), at(36*time.Hour, 3), at(24*time.Hour, 22), at(12*time.Hour, 11)}
	f := Predict(l, hist, t0)
	spentPerDay := (1 + 11 + 1) / 2.0
	if !near(f.Rate*24, spentPerDay) || f.Verdict != RunsOut {
		t.Fatalf("rate %.2f/day verdict %v", f.Rate*24, f.Verdict)
	}
	if !strings.Contains(verdictText(l, f), "runs out in ~") {
		t.Fatalf("text %q", verdictText(l, f))
	}
	steady := []Sample{at(72*time.Hour, 10), at(time.Hour, 10)}
	if f := Predict(l, steady, t0); f.Verdict != OnTrack || verdictText(l, f) != "✓ no spending in the last week" {
		t.Fatalf("steady: %v %q", f.Verdict, verdictText(l, f))
	}
	if f := Predict(Limit{Kind: KindBalance}, nil, t0); f.Verdict != Maxed {
		t.Fatalf("empty balance: %v", f.Verdict)
	}
}

func TestPredictSpendWithoutBudget(t *testing.T) {
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC) // 10 days into a 31-day month
	l := spendLimit("openai-api/month", 50, 0, now)
	f := Predict(l, nil, now)
	if f.Verdict != Idle || !near(f.AtReset, 155) {
		t.Fatalf("%+v", f)
	}
	if got := verdictText(l, f); got != "○ ~$155 by month end · no budget set" {
		t.Fatalf("text %q", got)
	}
	l = spendLimit("openai-api/month", 50, 100, now)
	if f := Predict(l, nil, now); f.Verdict != RunsOut || !strings.Contains(verdictText(l, f), "over budget in ~") {
		t.Fatalf("budget: %v %q", f.Verdict, verdictText(l, f))
	}
}

func TestMoneyAndKeys(t *testing.T) {
	for v, want := range map[float64]string{12.4: "$12.40", 1250: "$1,250", 0: "$0.00", -3: "-$3.00", 150.4: "$150"} {
		if got := money(v, "USD"); got != want {
			t.Errorf("money(%v) = %q, want %q", v, got, want)
		}
	}
	if got := money(8.2, "CHF"); got != "8.20 CHF" {
		t.Errorf("CHF %q", got)
	}
	if got := maskKey("sk-1234567890abcdef"); got != "sk-1…cdef" {
		t.Errorf("mask %q", got)
	}

	isolate(t)
	if err := setKey("claude", "x"); err == nil {
		t.Error("claude uses a sign-in, not a key")
	}
	if err := setKey("nope", "x"); err == nil {
		t.Error("unknown provider accepted")
	}
	setKey("zai", "zai-key-123456789")
	if k, from := apiKey(provider(t, "zai")); k != "zai-key-123456789" || from != "config" {
		t.Fatalf("saved key %q from %q", k, from)
	}
	if st, _ := os.Stat(configPath()); st.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("config is readable by others: %v", st.Mode())
	}
	if !strings.Contains(keyList(), "zai-…6789") {
		t.Errorf("key list:\n%s", keyList())
	}
	if err := removeKey("zai"); err != nil {
		t.Fatal(err)
	}
	if k, _ := apiKey(provider(t, "zai")); k != "" {
		t.Fatal("key not removed")
	}
}

func TestVisibleHidesProvidersNotSetUp(t *testing.T) {
	reports := []Report{{Provider: "Claude"}, {Provider: "DeepSeek", Missing: true}, {Provider: "Kimi API", Missing: true}}
	shown, missing := visible(reports, false)
	if len(shown) != 1 || strings.Join(missing, ",") != "deepseek,moonshot" {
		t.Fatalf("shown %d missing %v", len(shown), missing)
	}
	if shown, _ := visible(reports, true); len(shown) != 3 {
		t.Fatal("providers asked for by name should all show")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
