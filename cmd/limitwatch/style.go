package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	cAccent  = lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#A78BFA"}
	cAccent2 = lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#67E8F9"}
	cDim     = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#6B7280"}
	cFaint   = lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#4B5563"}
	cText    = lipgloss.AdaptiveColor{Light: "#111827", Dark: "#E5E7EB"}
	cBorder  = lipgloss.AdaptiveColor{Light: "#D1D5DB", Dark: "#3F3F55"}
	cErr     = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	cWarn    = lipgloss.AdaptiveColor{Light: "#C2410C", Dark: "#FDBA74"}
	cOK      = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#86EFAC"}
	cIdle    = lipgloss.AdaptiveColor{Light: "#1D4ED8", Dark: "#60A5FA"}

	sLogo      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1E1B2E")).Background(cAccent).Padding(0, 1)
	sDim       = lipgloss.NewStyle().Foreground(cDim)
	sFaint     = lipgloss.NewStyle().Foreground(cFaint)
	sText      = lipgloss.NewStyle().Foreground(cText)
	sBold      = lipgloss.NewStyle().Foreground(cText).Bold(true)
	sKey       = lipgloss.NewStyle().Foreground(cAccent2).Bold(true)
	sErr       = lipgloss.NewStyle().Foreground(cErr)
	sOK        = lipgloss.NewStyle().Foreground(cOK)
	sPaneTitle = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
)

// verdictColor is the color of the bar and verdict for each outcome.
func verdictColor(v Verdict) lipgloss.AdaptiveColor {
	switch v {
	case OnTrack:
		return cOK
	case Tight:
		return cWarn
	case RunsOut, Maxed:
		return cErr
	default:
		return cIdle
	}
}

var verdictGlyph = map[Verdict]string{Idle: "○", OnTrack: "✓", Tight: "◐", RunsOut: "⚠", Maxed: "✕"}

// verdictText is the one-line answer to "will this last until the reset?",
// or for a balance, "how long will it last?".
func verdictText(l Limit, f Forecast) string {
	g := verdictGlyph[f.Verdict] + " "
	switch {
	case l.Kind == KindBalance:
		switch f.Verdict {
		case Maxed:
			return g + "out of credit"
		case Idle:
			return g + "spend rate after an hour of readings"
		case RunsOut:
			return g + "runs out in ~" + humanDur(f.EmptyIn)
		}
		if f.Rate <= 0 {
			return g + "no spending in the last week"
		}
		return g + "lasts ~" + humanDur(f.EmptyIn)
	case l.Kind == KindSpend && l.Total <= 0:
		if f.Left == 0 {
			return g + "no budget set"
		}
		return g + "~" + money(f.AtReset, l.Unit) + " by month end · no budget set"
	case l.Kind == KindSpend:
		end := fmt.Sprintf("~%s of %s by month end", money(f.AtReset/100*l.Total, l.Unit), money(l.Total, l.Unit))
		switch f.Verdict {
		case Maxed:
			return g + "budget used up"
		case RunsOut:
			return g + fmt.Sprintf("over budget in ~%s · %s before month end", humanDur(f.EmptyIn), humanDur(f.Left-f.EmptyIn))
		case Tight:
			return g + "tight · " + end
		case OnTrack:
			return g + "on track · " + end
		}
	}
	switch f.Verdict {
	case Maxed:
		if f.Left > 0 {
			return g + "limit reached · back in " + humanDur(f.Left)
		}
		return g + "limit reached"
	case RunsOut:
		return g + fmt.Sprintf("runs out in ~%s · %s before reset", humanDur(f.EmptyIn), humanDur(f.Left-f.EmptyIn))
	case Tight:
		return g + "tight · " + atReset(f)
	case OnTrack:
		return g + "on track · " + atReset(f)
	default:
		if l.ResetsAt.IsZero() {
			return g + "not started"
		}
		return g + "idle"
	}
}

func atReset(f Forecast) string {
	s := fmt.Sprintf("~%.0f%% at reset", math.Min(f.AtReset, 100))
	if f.HasRecent && f.High-f.Low >= 5 {
		s += fmt.Sprintf(" (%.0f–%.0f%%)", math.Min(f.Low, 100), math.Min(f.High, 100))
	}
	return s
}

// paceText explains the numbers behind the verdict.
func paceText(l Limit, f Forecast) string {
	switch {
	case l.Kind == KindBalance:
		if !f.HasRecent {
			return "keep limitwatch open, or run `limitwatch watch`, to learn your spend rate"
		}
		return "spending " + money(f.Rate*24, l.Unit) + "/day over the last week"
	case l.Kind == KindSpend && l.Total <= 0:
		if f.AvgRate == 0 {
			return ""
		}
		return money(f.AvgRate*24, l.Unit) + "/day avg · set one with `budget` to get a verdict"
	case l.Kind == KindSpend && f.Left > 0:
		perDay := func(pctPerHour float64) string { return money(pctPerHour/100*l.Total*24, l.Unit) + "/day" }
		s := perDay(f.AvgRate) + " avg"
		if f.HasRecent {
			s = perDay(f.RecentRate) + " now, " + s
		}
		if f.Verdict != Maxed {
			s += " · budget " + perDay(f.SafeRate)
		}
		return s
	}
	if f.Verdict == Idle || f.Left == 0 {
		switch {
		case !l.ResetsAt.IsZero():
			return "nothing used yet"
		case l.Window > 0:
			return humanDur(l.Window) + " window · starts with your next message"
		}
		return ""
	}
	r := func(v float64) string { return rateFor(l, v) }
	s := r(f.AvgRate) + " avg"
	if f.HasRecent {
		s = r(f.RecentRate) + " now, " + s
	}
	if f.Verdict != Maxed {
		s += " · budget " + r(f.SafeRate)
	}
	return s
}

// rateFor shows a pace per hour, or per day for windows of a week or more.
func rateFor(l Limit, perHour float64) string {
	if l.Window >= 7*24*time.Hour {
		v := perHour * 24
		if v < 10 {
			return fmt.Sprintf("%.1f%%/day", v)
		}
		return fmt.Sprintf("%.0f%%/day", v)
	}
	return rate(perHour)
}

// countText is "79 / 2,000 completions" for limits the provider counts.
func countText(l Limit) string {
	if l.Kind != KindWindow || l.Total <= 0 || l.Unit == "" {
		return ""
	}
	return commas(l.Amount) + " / " + commas(l.Total) + " " + l.Unit
}

// valueText is the number beside the bar: percent used, or for a balance or
// a month's spend without a budget, the amount.
func valueText(l Limit) string {
	if l.Kind == KindBalance || (l.Kind == KindSpend && l.Total <= 0) {
		return money(l.Amount, l.Unit)
	}
	return pct(l.Used)
}

var currencySymbol = map[string]string{"USD": "$", "CNY": "¥", "EUR": "€", "GBP": "£", "JPY": "¥"}

// money formats an amount with its currency: "$12.40", "¥1,250", "8.20 CHF".
func money(v float64, unit string) string {
	neg := ""
	if v < 0 {
		neg, v = "-", -v
	}
	var s string
	switch {
	case v >= 1000:
		s = commas(v)
	case v >= 100:
		s = fmt.Sprintf("%.0f", v)
	default:
		s = fmt.Sprintf("%.2f", v)
	}
	if sym, ok := currencySymbol[strings.ToUpper(unit)]; ok {
		return neg + sym + s
	}
	return strings.TrimSpace(neg + s + " " + unit)
}

// commas rounds to a whole number and groups the thousands: 12,345.
func commas(v float64) string {
	s := fmt.Sprintf("%.0f", math.Round(v))
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}

func rate(r float64) string {
	if r < 10 {
		return fmt.Sprintf("%.1f%%/h", r)
	}
	return fmt.Sprintf("%.0f%%/h", r)
}

// bar draws used% solid, the projected growth until the reset shaded, and
// the rest as track.
func bar(l Limit, f Forecast, w int) string {
	if w <= 0 {
		return ""
	}
	cells := func(p float64) int { return int(math.Round(math.Max(0, math.Min(100, p)) / 100 * float64(w))) }
	used := cells(l.Used)
	if l.Used > 0 && used == 0 {
		used = 1
	}
	proj := used
	if f.Verdict != Idle && f.Verdict != Maxed {
		proj = max(used, cells(f.AtReset))
	}
	c := lipgloss.NewStyle().Foreground(verdictColor(f.Verdict))
	return c.Render(strings.Repeat("█", used)) +
		c.Faint(true).Render(strings.Repeat("▒", proj-used)) +
		sFaint.Render(strings.Repeat("░", w-proj))
}

func pct(v float64) string {
	if v > 0 && v < 1 {
		return "<1%"
	}
	return fmt.Sprintf("%.0f%%", math.Floor(v))
}

// humanDur prints a duration with its two largest units: "3d 4h", "1h 40m", "55m".
func humanDur(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	d = d.Round(time.Minute)
	days, hours, mins := int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

func humanAge(t, now time.Time) string {
	d := now.Sub(t)
	if d < time.Minute {
		return "just now"
	}
	return humanDur(d) + " ago"
}
