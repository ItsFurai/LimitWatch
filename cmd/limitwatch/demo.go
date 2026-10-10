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
			Provider: "Claude", Plan: "Max 5x", Fetched: now,
			Limits: []Limit{
				{ID: "claude/five_hour", Name: "Session", Used: 78, Window: 5 * h, ResetsAt: now.Add(100 * time.Minute)},
				{ID: "claude/seven_day", Name: "Weekly", Used: 42, Window: 7 * 24 * h, ResetsAt: now.Add(76 * h)},
				{ID: "claude/seven_day_opus", Name: "Weekly Opus", Used: 30, Window: 7 * 24 * h, ResetsAt: now.Add(76 * h)},
			},
		},
		{
			Provider: "Codex", Plan: "Plus", Fetched: now,
			Limits: []Limit{
				{ID: "codex/primary", Name: "5-hour", Used: 23, Window: 5 * h, ResetsAt: now.Add(3*h + 12*time.Minute)},
				{ID: "codex/secondary", Name: "Weekly", Used: 12, Window: 7 * 24 * h, ResetsAt: now.Add(5*24*h + 2*h)},
			},
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
