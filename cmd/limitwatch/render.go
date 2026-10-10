package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

const nameW = 14

func barWidth(w int) int { return max(10, min(32, w-nameW-8-72)) }

// limitLines renders one limit as two lines:
//
//	Session       ███████▒▒▒░░░░░░  78%  ⚠ runs out in ~55m · 45m before reset
//	              ▁▂▂▃▅▆▇█··········       resets in 1h 40m (21:30) · 34%/h now, 23%/h avg · budget 13%/h
//
// The first line is the bar and the verdict; the second is how usage moved
// through the window so far, when it resets, and the pace behind the verdict.
func limitLines(l Limit, f Forecast, samples []Sample, now time.Time, w int) []string {
	bw := barWidth(w)
	c := lipgloss.NewStyle().Foreground(verdictColor(f.Verdict))
	name := ansi.Truncate(l.Name, nameW-2, "…")
	first := " " + sBold.Render(fmt.Sprintf("%-*s", nameW-1, name)) + bar(l, f, bw) + "  " +
		sText.Render(fmt.Sprintf("%4s", pct(l.Used))) + "  " + c.Render(verdictText(l, f))

	var info []string
	if f.Left > 0 {
		info = append(info, "resets in "+humanDur(f.Left)+sFaint.Render(" ("+resetClock(l.ResetsAt, now)+")"))
	}
	if p := paceText(l, f); p != "" {
		info = append(info, sDim.Render(p))
	}
	second := " " + strings.Repeat(" ", nameW-1) + timeline(l, f, samples, now, bw) + "        " +
		sDim.Render(strings.Join(info, sFaint.Render(" · ")))
	return []string{ansi.Truncate(first, w, "…"), ansi.Truncate(second, w, "…")}
}

func resetClock(t, now time.Time) string {
	t = t.Local()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("Mon 15:04")
}

var sparks = []rune("▁▂▃▄▅▆▇█")

// timeline spreads the window across w cells, from its start to its reset.
// Past cells show the recorded usage, the cell for now is marked, and the
// rest of the window is dotted.
func timeline(l Limit, f Forecast, samples []Sample, now time.Time, w int) string {
	if l.ResetsAt.IsZero() || l.Window == 0 || w <= 0 {
		return sFaint.Render(strings.Repeat("·", max(0, w)))
	}
	start := l.ResetsAt.Add(-l.Window)
	cell := l.Window / time.Duration(w)
	nowCell := min(w-1, max(0, int(now.Sub(start)/cell)))
	level := make([]float64, w)
	for i := range level {
		level[i] = -1
	}
	for _, s := range samples {
		if i := int(s.Time().Sub(start) / cell); i >= 0 && i < w {
			level[i] = math.Max(level[i], s.Used)
		}
	}
	level[nowCell] = math.Max(level[nowCell], l.Used)
	c := lipgloss.NewStyle().Foreground(verdictColor(f.Verdict)).Faint(true)
	var b strings.Builder
	for i := 0; i < w; i++ {
		switch {
		case i > nowCell:
			b.WriteString(sFaint.Render("·"))
		case level[i] < 0:
			b.WriteString(sFaint.Render(" "))
		default:
			k := int(math.Round(level[i] / 100 * float64(len(sparks)-1)))
			b.WriteString(c.Render(string(sparks[max(0, min(len(sparks)-1, k))])))
		}
	}
	return b.String()
}

// reportLines renders a provider's limits, or why there are none.
func reportLines(r Report, hist History, now time.Time, w int) []string {
	var hint []string
	if r.Hint != "" {
		hint = []string{" " + sKey.Render("→ ") + sDim.Render(r.Hint)}
	}
	switch {
	case r.Missing:
		return append([]string{" " + sDim.Render(r.Err)}, hint...)
	case len(r.Limits) == 0 && r.Err != "":
		return append([]string{" " + sErr.Render("⚠ "+r.Err)}, hint...)
	}
	var lines []string
	for i, l := range r.Limits {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, limitLines(l, Predict(l, hist.inWindow(l), now), hist.inWindow(l), now, w)...)
	}
	if r.Err != "" {
		lines = append(append(lines, "", " "+sErr.Render("⚠ "+r.Err)), hint...)
	}
	return lines
}

func reportInfo(r Report) string {
	var parts []string
	if r.Plan != "" {
		parts = append(parts, r.Plan)
	}
	if r.Source != "" {
		parts = append(parts, r.Source)
	}
	if len(r.Running) > 0 {
		parts = append(parts, "● "+strings.Join(r.Running, ", "))
	}
	return strings.Join(parts, " · ")
}

// renderStatus is the output of `limitwatch status`.
func renderStatus(reports []Report, hist History, now time.Time, w int) string {
	var b strings.Builder
	for i, r := range reports {
		if i > 0 {
			b.WriteByte('\n')
		}
		head := sPaneTitle.Render(r.Provider)
		if info := reportInfo(r); info != "" {
			head += "  " + sDim.Render(info)
		}
		b.WriteString(head + "\n")
		for _, l := range reportLines(r, hist, now, w) {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// renderStatusline is a single line: "Claude 5h 78% ⚠55m · 7d 31% ✓".
func renderStatusline(reports []Report, hist History, now time.Time) string {
	var parts []string
	for _, r := range reports {
		if len(r.Limits) == 0 {
			if !r.Missing {
				parts = append(parts, sDim.Render(r.Provider+" ?"))
			}
			continue
		}
		var ls []string
		for _, l := range r.Limits {
			f := Predict(l, hist.inWindow(l), now)
			c := lipgloss.NewStyle().Foreground(verdictColor(f.Verdict))
			s := sDim.Render(shortWindow(l)+" ") + c.Render(pct(l.Used))
			switch f.Verdict {
			case RunsOut:
				s += c.Render(" ⚠" + humanDur(f.EmptyIn))
			case Maxed:
				s += c.Render(" ✕" + humanDur(f.Left))
			case Tight:
				s += c.Render(" ◐")
			}
			ls = append(ls, s)
		}
		p := strings.Join(ls, sFaint.Render(" · "))
		if len(reports) > 1 {
			p = sDim.Render(r.Provider+" ") + p
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, sFaint.Render("  │  "))
}

// shortWindow names a limit in two or three characters for the status line.
func shortWindow(l Limit) string {
	short := map[string]string{"Weekly Opus": "op", "Weekly Sonnet": "so", "Weekly apps": "ap"}
	if s, ok := short[l.Name]; ok {
		return s
	}
	switch {
	case l.Window == 0:
		return strings.ToLower(l.Name[:min(2, len(l.Name))])
	case l.Window < 24*time.Hour:
		return fmt.Sprintf("%dh", int(l.Window.Round(time.Hour).Hours()))
	default:
		return fmt.Sprintf("%dd", int(l.Window.Round(24*time.Hour).Hours()/24))
	}
}

func termWidth() int {
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		return w
	}
	return 120
}
