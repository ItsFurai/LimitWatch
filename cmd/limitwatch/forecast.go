package main

import (
	"math"
	"time"
)

type Verdict int

const (
	Idle    Verdict = iota // window not started, or nothing used yet
	OnTrack                // projected to stay under 90% by the reset
	Tight                  // projected to land between 90% and 100%
	RunsOut                // projected to hit 100% before the reset
	Maxed                  // already at 100%
)

func (v Verdict) String() string {
	return [...]string{"idle", "on track", "tight", "runs out", "maxed"}[v]
}

// Forecast is the prediction for one limit.
type Forecast struct {
	Verdict    Verdict
	Left       time.Duration // until the reset
	AvgRate    float64       // %/hour since the window started
	RecentRate float64       // %/hour over the recent stretch, from history
	HasRecent  bool
	Rate       float64       // the pace the projection uses
	AtReset    float64       // projected % at the reset (can exceed 100)
	Low, High  float64       // projected % at the reset for the slower and faster of the two paces
	EmptyIn    time.Duration // time until 100% at Rate (RunsOut only)
	SafeRate   float64       // the fastest pace that still lasts until the reset
}

const tightAt = 90.0

// Predict projects a limit to its reset. It uses two paces:
//
//   - the average since the window started (used% ÷ time elapsed), which
//     works from a single reading, and
//   - the recent pace, a least-squares slope over the last fifth of the
//     window (an hour for a 5-hour window, about a day and a half for a
//     week), which needs a few readings from the history.
//
// The recent pace drives the verdict when it's known, since it reflects how
// you're working right now; the average gives the other end of the range.
func Predict(l Limit, samples []Sample, now time.Time) Forecast {
	switch {
	case l.Kind == KindBalance:
		return predictBalance(l, samples, now)
	case l.Kind == KindSpend && l.Total <= 0:
		return predictSpend(l, now)
	}
	var f Forecast
	if l.Used >= 100 {
		f.Verdict = Maxed
	}
	if l.ResetsAt.IsZero() || !l.ResetsAt.After(now) {
		if f.Verdict != Maxed {
			f.Verdict = Idle
		}
		return f
	}
	f.Left = l.ResetsAt.Sub(now)
	left := f.Left.Hours()
	f.SafeRate = (100 - l.Used) / left

	if l.Window > 0 {
		elapsed := (l.Window - f.Left).Hours()
		if elapsed >= 5.0/60 {
			f.AvgRate = l.Used / elapsed
		}
	}
	f.RecentRate, f.HasRecent = recentRate(l, samples, now)

	f.Rate = f.AvgRate
	if f.HasRecent {
		f.Rate = f.RecentRate
	}
	project := func(rate float64) float64 { return l.Used + rate*left }
	f.AtReset = project(f.Rate)
	f.Low, f.High = f.AtReset, f.AtReset
	if f.HasRecent {
		f.Low = project(math.Min(f.AvgRate, f.RecentRate))
		f.High = project(math.Max(f.AvgRate, f.RecentRate))
	}

	switch {
	case f.Verdict == Maxed:
	case l.Used == 0 && f.Rate == 0:
		f.Verdict = Idle
	case f.AtReset >= 100 && f.Rate > 0:
		f.Verdict = RunsOut
		f.EmptyIn = time.Duration((100 - l.Used) / f.Rate * float64(time.Hour))
	case f.AtReset >= tightAt:
		f.Verdict = Tight
	default:
		f.Verdict = OnTrack
	}
	return f
}

// Balances never reset, so the question is how long they last. Spending is
// the sum of the drops between readings over the last week; a rise is a
// top-up and doesn't count. Rate is then in the balance's currency per hour.
const (
	balanceLookback = 7 * 24 * time.Hour
	balanceRunsOut  = 3 * 24 * time.Hour  // red below this
	balanceTight    = 14 * 24 * time.Hour // orange below this
)

func predictBalance(l Limit, samples []Sample, now time.Time) Forecast {
	var f Forecast
	if l.Amount <= 0 {
		f.Verdict = Maxed
		return f
	}
	var first time.Time
	prev, spent, n := math.NaN(), 0.0, 0
	for _, s := range samples {
		t := s.Time()
		if t.Before(now.Add(-balanceLookback)) || t.After(now) {
			continue
		}
		if n == 0 {
			first = t
		} else if s.Amount < prev {
			spent += prev - s.Amount
		}
		prev = s.Amount
		n++
	}
	if n > 0 && l.Amount < prev {
		spent += prev - l.Amount
	}
	if n == 0 || now.Sub(first) < time.Hour {
		f.Verdict = Idle // not enough readings to know the spend rate yet
		return f
	}
	f.HasRecent = true
	f.Rate = spent / now.Sub(first).Hours()
	f.RecentRate = f.Rate
	if f.Rate <= 0 {
		f.Verdict = OnTrack
		return f
	}
	f.EmptyIn = time.Duration(l.Amount / f.Rate * float64(time.Hour))
	switch {
	case f.EmptyIn < balanceRunsOut:
		f.Verdict = RunsOut
	case f.EmptyIn < balanceTight:
		f.Verdict = Tight
	default:
		f.Verdict = OnTrack
	}
	return f
}

// predictSpend projects a month's API spend when there's no budget to
// measure it against. AtReset and the rates are in currency, not percent.
func predictSpend(l Limit, now time.Time) Forecast {
	f := Forecast{Verdict: Idle}
	if !l.ResetsAt.After(now) {
		return f
	}
	f.Left = l.ResetsAt.Sub(now)
	if elapsed := (l.Window - f.Left).Hours(); elapsed >= 1 {
		f.AvgRate = l.Amount / elapsed
	}
	f.Rate = f.AvgRate
	f.AtReset = l.Amount + f.Rate*f.Left.Hours()
	f.Low, f.High = f.AtReset, f.AtReset
	return f
}

// recentRate fits a line through this window's readings from the last
// fifth of the window, plus the current one, and returns its slope in %/hour.
func recentRate(l Limit, samples []Sample, now time.Time) (float64, bool) {
	if l.Window == 0 {
		return 0, false
	}
	span := l.Window / 5
	from := now.Add(-span)
	type pt struct{ x, y float64 } // hours before now, percent
	pts := []pt{{0, l.Used}}
	oldest := 0.0
	for _, s := range samples {
		t := s.Time()
		if t.Before(from) || t.After(now) {
			continue
		}
		x := -now.Sub(t).Hours()
		pts = append(pts, pt{x, s.Used})
		oldest = math.Min(oldest, x)
	}
	// Ask for readings covering at least a sixth of the span (10 minutes of
	// a 5-hour window), so two readings a few seconds apart don't count.
	if len(pts) < 3 || -oldest < span.Hours()/6 {
		return 0, false
	}
	var sx, sy, sxx, sxy float64
	n := float64(len(pts))
	for _, p := range pts {
		sx += p.x
		sy += p.y
		sxx += p.x * p.x
		sxy += p.x * p.y
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return 0, false
	}
	return math.Max(0, (n*sxy-sx*sy)/den), true
}
