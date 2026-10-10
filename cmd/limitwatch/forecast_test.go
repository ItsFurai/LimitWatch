package main

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func session(used float64, left time.Duration) Limit {
	return Limit{ID: "claude/five_hour", Name: "Session", Used: used, Window: 5 * time.Hour, ResetsAt: t0.Add(left)}
}

// readings returns one sample per minute over the last `span`, rising
// linearly at ratePerHour and ending at l.Used.
func readings(l Limit, span time.Duration, ratePerHour float64) []Sample {
	var out []Sample
	for d := span; d > 0; d -= time.Minute {
		out = append(out, Sample{T: t0.Add(-d).Unix(), ID: l.ID, Used: l.Used - ratePerHour*d.Hours(), Reset: l.ResetsAt.Unix()})
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.5 }

func TestPredictAverageOnly(t *testing.T) {
	// 2h into a 5h window at 20%: 10%/h, so 50% at the reset.
	f := Predict(session(20, 3*time.Hour), nil, t0)
	if f.Verdict != OnTrack || !near(f.AvgRate, 10) || !near(f.AtReset, 50) || f.HasRecent {
		t.Fatalf("got %+v", f)
	}
	if !near(f.SafeRate, 80.0/3) {
		t.Fatalf("safe rate %v", f.SafeRate)
	}
}

func TestPredictRunsOut(t *testing.T) {
	// 3h into the window at 80%: 26.7%/h, so 100% in 45 minutes, well before the reset.
	f := Predict(session(80, 2*time.Hour), nil, t0)
	if f.Verdict != RunsOut {
		t.Fatalf("verdict %v", f.Verdict)
	}
	if f.EmptyIn.Round(time.Minute) != 45*time.Minute {
		t.Fatalf("empty in %v", f.EmptyIn)
	}
}

func TestPredictRecentPaceWins(t *testing.T) {
	// Average is a calm 15%/h, but the last hour ran at 30%/h.
	l := session(30, 3*time.Hour)
	f := Predict(l, readings(l, time.Hour, 30), t0)
	if !f.HasRecent || !near(f.RecentRate, 30) {
		t.Fatalf("recent %v %v", f.HasRecent, f.RecentRate)
	}
	if f.Verdict != RunsOut || !near(f.Rate, 30) {
		t.Fatalf("got %+v", f)
	}
	if !near(f.Low, 30+15*3) || !near(f.High, 30+30*3) {
		t.Fatalf("range %v–%v", f.Low, f.High)
	}
}

func TestPredictIgnoresOldWindowsAndShortSpans(t *testing.T) {
	l := session(30, 2*time.Hour)
	// Readings from a few seconds ago don't make a pace.
	short := []Sample{
		{T: t0.Add(-20 * time.Second).Unix(), ID: l.ID, Used: 29, Reset: l.ResetsAt.Unix()},
		{T: t0.Add(-10 * time.Second).Unix(), ID: l.ID, Used: 29.5, Reset: l.ResetsAt.Unix()},
	}
	if f := Predict(l, short, t0); f.HasRecent {
		t.Fatalf("pace from %d seconds of data: %+v", 20, f)
	}
	// Readings from the previous window are filtered out by History.inWindow.
	h := History{l.ID: readings(Limit{ID: l.ID, Used: 90, ResetsAt: t0.Add(-time.Minute)}, time.Hour, 50)}
	if got := h.inWindow(l); len(got) != 0 {
		t.Fatalf("kept %d samples from the old window", len(got))
	}
	h = History{l.ID: readings(l, time.Hour, 30)}
	if got := h.inWindow(l); len(got) != 60 {
		t.Fatalf("kept %d of 60 samples", len(got))
	}
}

func TestPredictEdges(t *testing.T) {
	if f := Predict(Limit{Name: "Session", Window: 5 * time.Hour}, nil, t0); f.Verdict != Idle {
		t.Fatalf("not started: %v", f.Verdict)
	}
	if f := Predict(session(100, time.Hour), nil, t0); f.Verdict != Maxed || f.Left != time.Hour {
		t.Fatalf("maxed: %+v", f)
	}
	if f := Predict(session(50, -time.Minute), nil, t0); f.Verdict != Idle {
		t.Fatalf("past reset: %v", f.Verdict)
	}
	// 93% projected is tight, not out.
	if f := Predict(session(37.2, 3*time.Hour), nil, t0); f.Verdict != Tight {
		t.Fatalf("37.2%% after 2h → %.0f%%: %v", f.AtReset, f.Verdict)
	}
}

func TestHumanDur(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second:               "<1m",
		55 * time.Minute:               "55m",
		100 * time.Minute:              "1h 40m",
		2 * time.Hour:                  "2h",
		76 * time.Hour:                 "3d 4h",
		48 * time.Hour:                 "2d",
		48*time.Hour + 29*time.Second:  "2d",
		5*time.Hour + 59*time.Minute:   "5h 59m",
		24*time.Hour - 20*time.Second:  "1d",
		24*time.Hour + 30*time.Minute:  "1d",
		24*time.Hour + 90*time.Minute:  "1d 1h",
		7 * 24 * time.Hour:             "7d",
		7*24*time.Hour + 5*time.Minute: "7d",
	}
	for d, want := range cases {
		if got := humanDur(d); got != want {
			t.Errorf("humanDur(%v) = %q, want %q", d, got, want)
		}
	}
}
