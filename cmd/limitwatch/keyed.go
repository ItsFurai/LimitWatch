package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ---------- Z.ai / Zhipu GLM Coding Plan ----------

// The GLM Coding Plan reports its quotas through the monitor endpoint Z.ai's
// own plan-usage plugin reads. It takes the API key as is, without "Bearer".
var zaiBaseURL = map[string]string{
	"ZAI_API_KEY":   "https://api.z.ai/api",
	"ZHIPU_API_KEY": "https://open.bigmodel.cn/api",
	"config":        "https://api.z.ai/api",
}

func fetchZai(ctx context.Context, p Provider) Report {
	key, from := apiKey(p)
	if key == "" {
		return failed(p.Name, needKey(p))
	}
	base := zaiBaseURL[from]
	if b := firstEnv("ZAI_BASE_URL"); b != "" {
		base = strings.TrimSuffix(b, "/")
	}
	r := Report{Provider: p.Name, Fetched: time.Now()}
	var raw json.RawMessage
	if err := getJSON(ctx, base+"/monitor/usage/quota/limit", map[string]string{"Authorization": key}, nil, &raw); err != nil {
		r.Err = err.Error()
		return r
	}
	var err error
	r.Limits, err = parseZai(raw)
	if err != nil {
		r.Err = err.Error()
	}
	return r
}

func parseZai(body []byte) ([]Limit, error) {
	var z struct {
		Success *bool  `json:"success"`
		Msg     string `json:"msg"`
		Data    struct {
			Limits []struct {
				Type          string  `json:"type"`
				Percentage    flexNum `json:"percentage"`
				Usage         flexNum `json:"usage"`
				CurrentValue  flexNum `json:"currentValue"`
				NextResetTime any     `json:"nextResetTime"`
			} `json:"limits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &z); err != nil {
		return nil, errors.New("unexpected reply from the Z.ai quota API: " + err.Error())
	}
	if z.Success != nil && !*z.Success {
		return nil, errors.New("Z.ai: " + z.Msg)
	}
	var out []Limit
	for _, l := range z.Data.Limits {
		lim := Limit{ID: "zai/" + strings.ToLower(l.Type), Used: clampPct(float64(l.Percentage)), ResetsAt: parseTimeAny(l.NextResetTime)}
		switch l.Type {
		case "TOKENS_LIMIT":
			lim.Name, lim.Window = "5-hour", 5*time.Hour
		case "TIME_LIMIT":
			// Web search, reader and other tool calls, counted per month.
			lim.Name, lim.Window, lim.Unit = "Monthly tools", 30*24*time.Hour, "calls"
			lim.Amount, lim.Total = float64(l.CurrentValue), float64(l.Usage)
		default:
			lim.Name = keyName(strings.ToLower(strings.TrimSuffix(l.Type, "_LIMIT")))
		}
		out = append(out, lim)
	}
	if len(out) == 0 {
		return nil, errors.New("the Z.ai quota API returned no limits")
	}
	return out, nil
}

// ---------- DeepSeek ----------

var deepseekURL = "https://api.deepseek.com/user/balance"

func fetchDeepSeek(ctx context.Context, p Provider) Report {
	key, _ := apiKey(p)
	if key == "" {
		return failed(p.Name, needKey(p))
	}
	r := Report{Provider: p.Name, Fetched: time.Now()}
	var b struct {
		IsAvailable  bool `json:"is_available"`
		BalanceInfos []struct {
			Currency       string  `json:"currency"`
			TotalBalance   flexNum `json:"total_balance"`
			GrantedBalance flexNum `json:"granted_balance"`
		} `json:"balance_infos"`
	}
	if err := getJSON(ctx, deepseekURL, map[string]string{"Authorization": "Bearer " + key}, nil, &b); err != nil {
		r.Err = err.Error()
		return r
	}
	for _, bi := range b.BalanceInfos {
		r.Limits = append(r.Limits, Limit{ID: "deepseek/" + strings.ToLower(bi.Currency), Name: "Balance",
			Kind: KindBalance, Unit: bi.Currency, Amount: float64(bi.TotalBalance)})
		if bi.GrantedBalance > 0 {
			r.Plan = money(float64(bi.GrantedBalance), bi.Currency) + " of it granted"
		}
	}
	if len(r.Limits) == 0 {
		r.Limits = []Limit{{ID: "deepseek/usd", Name: "Balance", Kind: KindBalance, Unit: "USD"}}
	}
	if !b.IsAvailable {
		r.Err = "DeepSeek says the balance is too low for API calls"
	}
	return r
}

// ---------- OpenRouter ----------

var openRouterURL = "https://openrouter.ai/api/v1"

func fetchOpenRouter(ctx context.Context, p Provider) Report {
	key, _ := apiKey(p)
	if key == "" {
		return failed(p.Name, needKey(p))
	}
	r := Report{Provider: p.Name, Fetched: time.Now()}
	auth := map[string]string{"Authorization": "Bearer " + key}

	var credits struct {
		Data struct {
			TotalCredits flexNum `json:"total_credits"`
			TotalUsage   flexNum `json:"total_usage"`
		} `json:"data"`
	}
	creditsErr := getJSON(ctx, openRouterURL+"/credits", auth, nil, &credits)
	if creditsErr == nil {
		r.Limits = append(r.Limits, Limit{ID: "openrouter/credits", Name: "Credits", Kind: KindBalance, Unit: "USD",
			Amount: float64(credits.Data.TotalCredits - credits.Data.TotalUsage)})
	}

	var k struct {
		Data struct {
			Label          string   `json:"label"`
			Limit          *flexNum `json:"limit"`
			LimitRemaining *flexNum `json:"limit_remaining"`
			LimitReset     *string  `json:"limit_reset"`
			IsFreeTier     bool     `json:"is_free_tier"`
		} `json:"data"`
	}
	keyErr := getJSON(ctx, openRouterURL+"/key", auth, nil, &k)
	if keyErr == nil {
		if k.Data.IsFreeTier {
			r.Plan = "Free tier"
		}
		if lim := openRouterKeyLimit(k.Data.Limit, k.Data.LimitRemaining, k.Data.LimitReset, time.Now()); lim != nil {
			r.Limits = append(r.Limits, *lim)
		}
	}
	if len(r.Limits) == 0 {
		err := keyErr
		if err == nil {
			err = creditsErr
		}
		if err == nil {
			err = errors.New("no credits or key limit to show")
		}
		r.Err = err.Error()
	}
	return r
}

// openRouterKeyLimit turns a key's spending cap into a limit: a window when
// the cap resets daily, weekly or monthly (UTC), or a balance when it never does.
func openRouterKeyLimit(limit, remaining *flexNum, reset *string, now time.Time) *Limit {
	if limit == nil || *limit <= 0 || remaining == nil {
		return nil
	}
	total, left := float64(*limit), float64(*remaining)
	l := Limit{ID: "openrouter/key", Name: "Key limit", Unit: "USD", Amount: total - left, Total: total,
		Used: clampPct((total - left) / total * 100)}
	day := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	period := ""
	if reset != nil {
		period = *reset
	}
	switch period {
	case "daily":
		l.ResetsAt, l.Window = day.AddDate(0, 0, 1), 24*time.Hour
	case "weekly":
		days := (8 - int(day.Weekday())) % 7 // to next Monday
		if days == 0 {
			days = 7
		}
		l.ResetsAt, l.Window = day.AddDate(0, 0, days), 7*24*time.Hour
	case "monthly":
		start := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
		l.ResetsAt = start.AddDate(0, 1, 0)
		l.Window = l.ResetsAt.Sub(start)
	default:
		l = Limit{ID: "openrouter/key", Name: "Key limit", Kind: KindBalance, Unit: "USD", Amount: left}
	}
	return &l
}
