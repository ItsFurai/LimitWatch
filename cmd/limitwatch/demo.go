package main

import (
	"math"
	"time"
)

// demoReports is made-up usage for --demo and the README screenshot: one
// limit that's about to run out, one that's tight, and a few that are fine.
func demoReports(now time.Time) []Report {
	h := time.Hour
	return []Report{
		{
			Provider: "Claude", Plan: "Max 5x", Fetched: now, Running: []string{"desktop app", "Claude Code"},
			Limits: []Limit{
				{ID: "claude/five_hour", Name: "Session", Used: 78, Window: 5 * h, ResetsAt: now.Add(100 * time.Minute)},
				{ID: "claude/seven_day", Name: "Weekly", Used: 42, Window: 7 * 24 * h, ResetsAt: now.Add(76 * h)},
				{ID: "claude/seven_day_opus", Name: "Weekly Opus", Used: 30, Window: 7 * 24 * h, ResetsAt: now.Add(76 * h)},
			},
		},
		{
			Provider: "Codex", Plan: "Plus", Fetched: now, Running: []string{"Codex"},
			Limits: []Limit{
				{ID: "codex/primary", Name: "5-hour", Used: 23, Window: 5 * h, ResetsAt: now.Add(3*h + 12*time.Minute)},
				{ID: "codex/secondary", Name: "Weekly", Used: 12, Window: 7 * 24 * h, ResetsAt: now.Add(5*24*h + 2*h)},
			},
		},
		{
			Provider: "Copilot", Plan: "Pro", Fetched: now,
			Limits: []Limit{
				{ID: "copilot/premium_interactions", Name: "Premium", Used: 58, Window: 31 * 24 * h, ResetsAt: now.Add(17 * 24 * h),
					Unit: "requests", Amount: 174, Total: 300},
			},
		},
		{
			Provider: "DeepSeek", Plan: "$2.00 of it granted", Fetched: now,
			Limits: []Limit{{ID: "deepseek/usd", Name: "Balance", Kind: KindBalance, Unit: "USD", Amount: 12.40}},
		},
	}
}

// demoHistory fakes the readings that would have led up to each demo limit,
// with a pace that picks up toward the end so the forecasts have something
// to say.
func demoHistory(reports []Report, now time.Time) History {
	h := History{}
	for _, r := range reports {
		for _, l := range r.Limits {
			if l.Kind == KindBalance {
				// A week of spending about $1.30 a day, after a $10 top-up four days ago.
				for i := 0; i <= 56; i++ {
					ago := time.Duration(56-i) * 3 * time.Hour
					amt := l.Amount + 1.3*ago.Hours()/24
					if ago > 4*24*time.Hour {
						amt -= 10
					}
					h[l.ID] = append(h[l.ID], Sample{T: now.Add(-ago).Unix(), ID: l.ID, Amount: amt})
				}
				continue
			}
			start := l.ResetsAt.Add(-l.Window)
			elapsed := now.Sub(start)
			steps := 60
			for i := 0; i < steps; i++ {
				frac := float64(i) / float64(steps)
				t := start.Add(time.Duration(frac * float64(elapsed)))
				// Usage grows slowly, then faster: used × frac^1.6.
				h[l.ID] = append(h[l.ID], Sample{T: t.Unix(), ID: l.ID, Used: l.Used * math.Pow(frac, 1.6), Reset: l.ResetsAt.Unix()})
			}
		}
	}
	return h
}
