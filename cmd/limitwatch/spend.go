package main

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

// API spend: what an organization has spent this calendar month (UTC), read
// from the cost reports of the OpenAI and Anthropic admin APIs. These need
// an admin key, which only an organization owner can create. With a budget
// (`limitwatch budget openai-api 100`) the month becomes a limit with a
// forecast like any other; without one, limitwatch projects the month's total.
var (
	openAICostsURL    = "https://api.openai.com/v1/organization/costs"
	anthropicCostsURL = "https://api.anthropic.com/v1/organizations/cost_report"
)

// spendLimit builds the month's limit from the amount spent so far.
func spendLimit(id string, spent, budget float64, now time.Time) Limit {
	start := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	l := Limit{ID: id, Name: "This month", Kind: KindSpend, Unit: "USD", Amount: spent, Total: budget,
		Window: end.Sub(start), ResetsAt: end}
	if budget > 0 {
		l.Used = clampPct(spent / budget * 100)
	}
	return l
}

func monthStart(now time.Time) time.Time {
	return time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
}

func fetchOpenAISpend(ctx context.Context, p Provider) Report {
	key, _ := apiKey(p)
	if key == "" {
		return failed(p.Name, needKey(p))
	}
	now := time.Now()
	r := Report{Provider: p.Name, Fetched: now}
	var total float64
	page := ""
	for i := 0; i < 5; i++ { // 31 daily buckets fit in one page; follow a few more just in case
		q := url.Values{"start_time": {strconv.FormatInt(monthStart(now).Unix(), 10)}, "bucket_width": {"1d"}, "limit": {"31"}}
		if page != "" {
			q.Set("page", page)
		}
		var raw json.RawMessage
		if err := getJSON(ctx, openAICostsURL+"?"+q.Encode(), map[string]string{"Authorization": "Bearer " + key}, nil, &raw); err != nil {
			r.Err = err.Error()
			return r
		}
		sum, next, err := sumOpenAICosts(raw)
		if err != nil {
			r.Err = err.Error()
			return r
		}
		total += sum
		if page = next; page == "" {
			break
		}
	}
	r.Limits = []Limit{spendLimit("openai-api/month", total, budgetFor(p.Key), now)}
	return r
}

func sumOpenAICosts(body []byte) (float64, string, error) {
	type result struct {
		Amount struct {
			Value flexNum `json:"value"`
		} `json:"amount"`
	}
	var c struct {
		Data []struct {
			Results []result `json:"results"`
			Result  []result `json:"result"` // some API versions use the singular
		} `json:"data"`
		HasMore  bool    `json:"has_more"`
		NextPage *string `json:"next_page"`
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return 0, "", err
	}
	var sum float64
	for _, b := range c.Data {
		for _, r := range append(b.Results, b.Result...) {
			sum += float64(r.Amount.Value)
		}
	}
	next := ""
	if c.HasMore && c.NextPage != nil {
		next = *c.NextPage
	}
	return sum, next, nil
}

func fetchAnthropicSpend(ctx context.Context, p Provider) Report {
	key, _ := apiKey(p)
	if key == "" {
		return failed(p.Name, needKey(p))
	}
	now := time.Now()
	r := Report{Provider: p.Name, Fetched: now}
	headers := map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"}
	var total float64
	page := ""
	for i := 0; i < 5; i++ {
		q := url.Values{"starting_at": {monthStart(now).Format(time.RFC3339)}, "bucket_width": {"1d"}, "limit": {"31"}}
		if page != "" {
			q.Set("page", page)
		}
		var raw json.RawMessage
		if err := getJSON(ctx, anthropicCostsURL+"?"+q.Encode(), headers, nil, &raw); err != nil {
			r.Err = err.Error()
			return r
		}
		sum, next, err := sumAnthropicCosts(raw)
		if err != nil {
			r.Err = err.Error()
			return r
		}
		total += sum
		if page = next; page == "" {
			break
		}
	}
	r.Limits = []Limit{spendLimit("anthropic-api/month", total, budgetFor(p.Key), now)}
	return r
}

// sumAnthropicCosts adds up a cost report page. Amounts are decimal strings
// in cents ("123.45" means $1.23).
func sumAnthropicCosts(body []byte) (float64, string, error) {
	var c struct {
		Data []struct {
			Results []struct {
				Amount flexNum `json:"amount"`
			} `json:"results"`
		} `json:"data"`
		HasMore  bool    `json:"has_more"`
		NextPage *string `json:"next_page"`
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return 0, "", err
	}
	var cents float64
	for _, b := range c.Data {
		for _, r := range b.Results {
			cents += float64(r.Amount)
		}
	}
	next := ""
	if c.HasMore && c.NextPage != nil {
		next = *c.NextPage
	}
	return cents / 100, next, nil
}
