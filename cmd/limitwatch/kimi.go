package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Kimi has two products with separate keys:
//   - Kimi Code, a membership with a weekly quota and a 5-hour rate window
//     (keys look like sk-kimi-…), read from its usage endpoint, and
//   - the Kimi (Moonshot) API platform, prepaid, read from its balance endpoint.
var (
	kimiCodeURL     = "https://api.kimi.com/coding/v1/usages"
	moonshotBaseURL = []string{"https://api.moonshot.ai", "https://api.moonshot.cn"}
)

// flexNum accepts numbers sent as JSON numbers or as strings.
type flexNum float64

func (f *flexNum) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexNum(v)
	return nil
}

type kimiQuota struct {
	Limit     flexNum  `json:"limit"`
	Used      *flexNum `json:"used"`
	Remaining *flexNum `json:"remaining"`
	ResetTime any      `json:"resetTime"`
}

func (q kimiQuota) usedPct() (float64, bool) {
	if q.Limit <= 0 {
		return 0, false
	}
	switch {
	case q.Used != nil:
		return clampPct(float64(*q.Used) / float64(q.Limit) * 100), true
	case q.Remaining != nil:
		return clampPct((float64(q.Limit) - float64(*q.Remaining)) / float64(q.Limit) * 100), true
	}
	return 0, false
}

func fetchKimiCode(ctx context.Context, p Provider) Report {
	key, _ := apiKey(p)
	if key == "" {
		return failed(p.Name, needKey(p))
	}
	r := Report{Provider: p.Name, Fetched: time.Now()}
	var raw json.RawMessage
	if err := getJSON(ctx, kimiCodeURL, map[string]string{"Authorization": "Bearer " + key}, nil, &raw); err != nil {
		r.Err = err.Error()
		return r
	}
	var err error
	r.Plan, r.Limits, err = parseKimiCode(raw)
	if err != nil {
		r.Err = err.Error()
	}
	return r
}

func parseKimiCode(body []byte) (string, []Limit, error) {
	var u struct {
		User struct {
			Membership struct {
				Level string `json:"level"`
			} `json:"membership"`
		} `json:"user"`
		Usage  *kimiQuota `json:"usage"`
		Limits []struct {
			Window struct {
				Duration flexNum `json:"duration"`
				TimeUnit string  `json:"timeUnit"`
			} `json:"window"`
			Detail *kimiQuota `json:"detail"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return "", nil, errors.New("unexpected reply from the Kimi Code API: " + err.Error())
	}
	plan := strings.ReplaceAll(title(strings.ToLower(strings.TrimPrefix(u.User.Membership.Level, "LEVEL_"))), "_", " ")
	var out []Limit
	for _, l := range u.Limits {
		if l.Detail == nil {
			continue
		}
		used, ok := l.Detail.usedPct()
		if !ok {
			continue
		}
		w := kimiWindow(float64(l.Window.Duration), l.Window.TimeUnit)
		out = append(out, Limit{ID: "kimi/" + strconv.Itoa(int(w.Minutes())) + "m", Name: windowName(w, 0), Used: used,
			Window: w, ResetsAt: parseTimeAny(l.Detail.ResetTime)})
	}
	if u.Usage != nil {
		if used, ok := u.Usage.usedPct(); ok {
			// The membership quota refreshes every 7 days from the subscription date.
			out = append(out, Limit{ID: "kimi/weekly", Name: "Weekly", Used: used, Window: 7 * 24 * time.Hour,
				ResetsAt: parseTimeAny(u.Usage.ResetTime)})
		}
	}
	if len(out) == 0 {
		return plan, nil, errors.New("the Kimi Code API returned no limits")
	}
	return plan, out, nil
}

func kimiWindow(n float64, unit string) time.Duration {
	per := map[string]time.Duration{
		"TIME_UNIT_SECOND": time.Second, "TIME_UNIT_MINUTE": time.Minute,
		"TIME_UNIT_HOUR": time.Hour, "TIME_UNIT_DAY": 24 * time.Hour,
	}[unit]
	return time.Duration(n * float64(per))
}

func fetchMoonshot(ctx context.Context, p Provider) Report {
	key, _ := apiKey(p)
	if key == "" {
		return failed(p.Name, needKey(p))
	}
	r := Report{Provider: p.Name, Fetched: time.Now()}
	bases := moonshotBaseURL
	if b := firstEnv("MOONSHOT_BASE_URL"); b != "" {
		bases = []string{strings.TrimSuffix(strings.TrimSuffix(b, "/"), "/v1")}
	}
	// Keys from platform.moonshot.ai (USD) and .cn (CNY) only work on their
	// own site, so try the next one when a key is rejected.
	var err error
	for _, base := range bases {
		var b struct {
			Data struct {
				Available flexNum `json:"available_balance"`
				Voucher   flexNum `json:"voucher_balance"`
				Cash      flexNum `json:"cash_balance"`
			} `json:"data"`
		}
		err = getJSON(ctx, base+"/v1/users/me/balance", map[string]string{"Authorization": "Bearer " + key}, nil, &b)
		if err != nil {
			continue
		}
		currency := "USD"
		if strings.HasSuffix(base, ".cn") {
			currency = "CNY"
		}
		r.Limits = []Limit{{ID: "moonshot/balance", Name: "Balance", Kind: KindBalance, Unit: currency, Amount: float64(b.Data.Available)}}
		if b.Data.Voucher > 0 {
			r.Plan = money(float64(b.Data.Voucher), currency) + " of it vouchers"
		}
		return r
	}
	r.Err = err.Error()
	return r
}
