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

// verdictText is the one-line answer to "will this last until the reset?".
func verdictText(l Limit, f Forecast) string {
	g := verdictGlyph[f.Verdict] + " "
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
	if f.Verdict == Idle || f.Left == 0 {
		if l.Window > 0 {
			return humanDur(l.Window) + " window · starts with your next message"
		}
		return ""
	}
	s := rate(f.AvgRate) + " avg"
	if f.HasRecent {
		s = rate(f.RecentRate) + " now, " + s
	}
	if f.Verdict != Maxed {
		s += " · budget " + rate(f.SafeRate)
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
