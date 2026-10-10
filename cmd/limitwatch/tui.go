package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type (
	tickMsg    time.Time
	fetchedMsg struct {
		reports []Report
		hist    History
		manual  bool
	}
)

type model struct {
	w, h     int
	ps       []Provider
	every    time.Duration
	reports  []Report
	hist     History
	fetching bool
	lastAt   time.Time
	frame    int
	flash    string
	flashAt  time.Time
	showHelp bool
	scroll   int
	now      func() time.Time
}

func newModel(ps []Provider, every time.Duration) model {
	return model{ps: ps, every: every, fetching: true, hist: History{}, now: time.Now}
}

func (m model) fetch(manual bool) tea.Cmd {
	ps := m.ps
	// Automatic refreshes reuse a reading another limitwatch took moments ago.
	maxAge := m.every / 2
	if manual {
		maxAge = 0
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		reports := fetchAll(ctx, ps, maxAge)
		return fetchedMsg{reports: reports, hist: historyFor(reports), manual: manual}
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Init() tea.Cmd { return tea.Batch(m.fetch(false), tick()) }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.clampScroll()
	case tickMsg:
		m.frame++
		var cmds []tea.Cmd
		if !m.fetching && time.Since(m.lastAt) >= m.every {
			m.fetching = true
			cmds = append(cmds, m.fetch(false))
		}
		if m.flash != "" && time.Since(m.flashAt) > 4*time.Second {
			m.flash = ""
		}
		return m, tea.Batch(append(cmds, tick())...)
	case fetchedMsg:
		m.fetching = false
		m.reports, m.hist, m.lastAt = msg.reports, msg.hist, time.Now()
		if msg.manual {
			m.flash, m.flashAt = "refreshed", time.Now()
		}
		m.clampScroll()
	case tea.KeyMsg:
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "r", "ctrl+r", "f5":
			if !m.fetching {
				m.fetching = true
				return m, m.fetch(true)
			}
		case "?", "f1":
			m.showHelp = true
		case "up", "k":
			m.scroll--
			m.clampScroll()
		case "down", "j":
			m.scroll++
			m.clampScroll()
		}
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scroll--
		case tea.MouseButtonWheelDown:
			m.scroll++
		}
		m.clampScroll()
	}
	return m, nil
}

func (m model) View() string {
	if m.w == 0 {
		return ""
	}
	if m.showHelp {
		return m.helpView()
	}
	body := m.bodyLines()
	h := max(1, m.h-2)
	body = body[min(m.scroll, len(body)):]
	if len(body) > h {
		body = body[:h]
	}
	for len(body) < h {
		body = append(body, "")
	}
	return m.headerView() + "\n" + strings.Join(body, "\n") + "\n" + m.footerView()
}

func (m *model) clampScroll() {
	m.scroll = max(0, min(m.scroll, len(m.bodyLines())-(m.h-2)))
}

// bodyLines is one box per provider, stacked.
func (m model) bodyLines() []string {
	if m.reports == nil {
		return centerMsg(m.h-2, m.w, sKey.Render(spinner(m.frame))+sDim.Render(" checking your limits…"))
	}
	now := m.now()
	var lines []string
	for _, r := range m.reports {
		inner := reportLines(r, m.hist, now, m.w-2)
		inner = append([]string{""}, append(inner, "")...)
		box := drawBox(r.Provider, reportInfo(r), inner, m.w, len(inner)+2, !r.Missing && r.Err == "")
		lines = append(lines, strings.Split(box, "\n")...)
	}
	return lines
}

func (m model) headerView() string {
	left := sLogo.Render("limitwatch") + "  " + sDim.Render("will your AI limits last until the reset?")
	var right string
	switch {
	case m.fetching:
		right = sKey.Render(spinner(m.frame)) + sDim.Render(" refreshing ")
	case !m.lastAt.IsZero():
		// A reading can come from the cache, so show the age of the oldest one.
		at := m.lastAt
		for _, r := range m.reports {
			if r.Fetched.Before(at) {
				at = r.Fetched
			}
		}
		right = sDim.Render(fmt.Sprintf("updated %s · every %s ", humanAge(at, m.now()), humanDur(m.every)))
	}
	if demoMode {
		right = sKey.Render("demo ") + right
	}
	return padBetween(left, right, m.w)
}

func (m model) footerView() string {
	left := ""
	if m.flash != "" {
		left = sOK.Render(" " + m.flash)
	} else {
		left = " " + legend()
	}
	keys := []string{"r", "refresh", "?", "help", "q", "quit"}
	right := ""
	for i := 0; i < len(keys); i += 2 {
		right += sKey.Render(keys[i]) + " " + sFaint.Render(keys[i+1]) + "  "
	}
	return padBetween(left, right, m.w)
}

// legend shows each verdict in its own color, from the same table the
// limit rows use, so it can't drift out of date.
func legend() string {
	var parts []string
	for _, v := range []Verdict{OnTrack, Tight, RunsOut, Idle} {
		parts = append(parts, lipgloss.NewStyle().Foreground(verdictColor(v)).Render(verdictGlyph[v]+" "+v.String()))
	}
	return strings.Join(parts, "  ")
}

func (m model) helpView() string {
	k := func(key, desc string) string {
		return "  " + sKey.Render(fmt.Sprintf("%-14s", key)) + sDim.Render(desc)
	}
	sec := func(s string) string { return "\n " + sPaneTitle.Render(s) }
	lines := []string{
		sec("Reading a limit"),
		k("█ ▒ ░", "used so far · projected use until the reset · still free"),
		k("▁▂▃▅▇ ···", "usage through the window so far · time left until the reset"),
		k("✓ on track", "projected to stay under 90% by the reset"),
		k("◐ tight", "projected to land between 90% and 100%"),
		k("⚠ runs out", "projected to hit 100% before the reset, and how early"),
		k("✕ maxed", "already at the limit"),
		sec("The forecast"),
		k("avg", "% used ÷ time since the window started; works from the first reading"),
		k("now", "slope of your readings over the last fifth of the window (an hour of a 5-hour"),
		k("", "window); used for the verdict once limitwatch has a few readings"),
		k("budget", "the fastest pace that still lasts until the reset"),
		k("", "keep limitwatch open, or run `limitwatch watch`, so it collects readings"),
		sec("Keys"),
		k("r", "refresh now"),
		k("↑ ↓ / wheel", "scroll"),
		k("q  esc", "quit"),
		"",
		"  " + sFaint.Render("history: "+historyPath()),
		"",
		"  " + sFaint.Render("press any key to close"),
	}
	return drawBox("limitwatch · help", version, strings.Split(strings.Join(lines, "\n"), "\n"), m.w, m.h, true)
}

// ---------- drawing helpers ----------

func drawBox(title, info string, lines []string, w, h int, focus bool) string {
	bc := cBorder
	if focus {
		bc = cAccent
	}
	bs := lipgloss.NewStyle().Foreground(bc)
	inner := w - 2
	top := "─"
	if title != "" {
		t := " " + title + " "
		t = ansi.Truncate(t, max(0, inner-2-lipgloss.Width(info)-3), "…")
		top += sPaneTitle.Render(t)
	}
	used := 1 + lipgloss.Width(top) - 1
	infoS := ""
	if info != "" {
		infoS = sDim.Render(" " + info + " ")
	}
	fill := inner - used - lipgloss.Width(infoS) - 1
	var b strings.Builder
	b.WriteString(bs.Render("╭─"))
	b.WriteString(strings.TrimPrefix(top, "─"))
	b.WriteString(bs.Render(strings.Repeat("─", max(0, fill))))
	b.WriteString(infoS)
	b.WriteString(bs.Render("─╮"))
	b.WriteByte('\n')
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(lines) {
			l = ansi.Truncate(lines[i], inner, "…")
		}
		pad := max(0, inner-lipgloss.Width(l))
		b.WriteString(bs.Render("│"))
		b.WriteString(l)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(bs.Render("│"))
		b.WriteByte('\n')
	}
	b.WriteString(bs.Render("╰" + strings.Repeat("─", max(0, inner)) + "╯"))
	return b.String()
}

func padBetween(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left+" "+right, w, "")
	}
	return left + strings.Repeat(" ", gap) + right
}

func centerMsg(h, w int, msgs ...string) []string {
	lines := make([]string, max(0, h/2-len(msgs)/2))
	for _, s := range msgs {
		pad := max(0, (w-lipgloss.Width(s))/2)
		lines = append(lines, strings.Repeat(" ", pad)+s)
	}
	return lines
}

func spinner(frame int) string {
	f := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return f[frame%len(f)]
}

func runTUI(ps []Provider, every time.Duration) error {
	_, err := tea.NewProgram(newModel(ps, every), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
