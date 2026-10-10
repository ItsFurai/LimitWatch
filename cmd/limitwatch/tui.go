package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
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
	flashErr bool
	flashAt  time.Time
	showHelp bool
	scroll   int
	now      func() time.Time

	// The command bar.
	cmdOn   bool
	input   textinput.Model
	cmdHist []string
	histPos int

	// When Claude Code's sign-in file or Codex's newest log last changed.
	stampClaude, stampCodex time.Time
}

func newModel(ps []Provider, every time.Duration) model {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "type a command, or help"
	in.PlaceholderStyle = sFaint
	in.TextStyle = sText
	in.Cursor.Style = lipgloss.NewStyle().Foreground(cAccent)
	m := model{ps: ps, every: every, fetching: true, hist: History{}, now: time.Now, input: in}
	if !demoMode {
		m.stampClaude, m.stampCodex = sourceStamps()
	}
	return m
}

func (m *model) setFlash(s string, isErr bool) tea.Cmd {
	m.flash, m.flashErr, m.flashAt = s, isErr, time.Now()
	return nil
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
		// Every 2 seconds, see if Claude Code or Codex has new numbers for us.
		if !demoMode && m.frame%2 == 0 {
			claude, codex := sourceStamps()
			changed := ""
			if !claude.Equal(m.stampClaude) && !claude.IsZero() {
				changed = "Claude Code updated its sign-in"
			}
			if !codex.Equal(m.stampCodex) && !codex.IsZero() {
				changed = "new reading from Codex"
			}
			m.stampClaude, m.stampCodex = claude, codex
			if changed != "" && !m.fetching {
				m.fetching = true
				m.setFlash(changed+"; refreshing", false)
				cmds = append(cmds, m.fetch(true))
			}
		}
		if m.flash != "" && time.Since(m.flashAt) > 5*time.Second {
			m.flash = ""
		}
		return m, tea.Batch(append(cmds, tick())...)
	case execDoneMsg:
		m.setFlash("back from "+strings.Fields(msg.label)[0]+"; refreshing", false)
		return m.refresh()
	case fetchedMsg:
		m.fetching = false
		m.reports, m.hist, m.lastAt = msg.reports, msg.hist, time.Now()
		if msg.manual && m.flash == "" {
			m.setFlash("refreshed", false)
		}
		m.clampScroll()
	case tea.KeyMsg:
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}
		if m.cmdOn {
			return m.commandKey(msg)
		}
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "r", "ctrl+r", "f5":
			return m.refresh()
		case ":", "/", "!":
			m.cmdOn, m.histPos = true, len(m.cmdHist)
			m.input.SetValue(strings.TrimLeft(msg.String(), ":/"))
			m.input.CursorEnd()
			return m, m.input.Focus()
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

// commandKey handles typing in the command bar.
func (m model) commandKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.cmdOn = false
		m.input.Blur()
		return m, nil
	case "enter":
		line := strings.TrimSpace(m.input.Value())
		m.cmdOn = false
		m.input.Blur()
		m.input.SetValue("")
		if line != "" && (len(m.cmdHist) == 0 || m.cmdHist[len(m.cmdHist)-1] != line) {
			m.cmdHist = append(m.cmdHist, line)
		}
		return m.runCommand(line)
	case "tab":
		m.input.SetValue(complete(m.input.Value()))
		m.input.CursorEnd()
		return m, nil
	case "up", "down":
		if len(m.cmdHist) == 0 {
			return m, nil
		}
		if msg.String() == "up" {
			m.histPos = max(0, m.histPos-1)
		} else {
			m.histPos = min(len(m.cmdHist), m.histPos+1)
		}
		val := ""
		if m.histPos < len(m.cmdHist) {
			val = m.cmdHist[m.histPos]
		}
		m.input.SetValue(val)
		m.input.CursorEnd()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
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
	if m.cmdOn {
		prompt := lipgloss.NewStyle().Foreground(cAccent).Bold(true).Render(" ❯ ")
		right := sFaint.Render(strings.Join(suggestions(m.input.Value()), "  ")) + "  " +
			sKey.Render("tab") + sFaint.Render(" complete  ") + sKey.Render("esc") + sFaint.Render(" cancel ")
		m.input.Width = max(10, m.w-lipgloss.Width(prompt)-lipgloss.Width(right)-2)
		return padBetween(prompt+m.input.View(), right, m.w)
	}
	left := " " + legend()
	if m.flash != "" {
		st := sOK
		if m.flashErr {
			st = sErr
		}
		left = st.Render(" " + m.flash)
	}
	keys := []string{":", "command", "!", "shell", "r", "refresh", "?", "help", "q", "quit"}
	right := ""
	for i := 0; i < len(keys); i += 2 {
		next := right + sKey.Render(keys[i]) + " " + sFaint.Render(keys[i+1]) + "  "
		if lipgloss.Width(left)+lipgloss.Width(next)+2 > m.w {
			break // drop whole hints rather than cutting one in half
		}
		right = next
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
		k("r", "refresh now (it also refreshes when Claude Code or Codex has new numbers)"),
		k(": or /", "open the command bar   ! runs a shell command   ↑↓ history   tab completes"),
		k("↑ ↓ / wheel", "scroll"),
		k("q  esc", "quit"),
		sec("Commands"),
	}
	for _, c := range commands {
		lines = append(lines, k(c.name, c.desc))
	}
	lines = append(lines,
		"",
		"  "+sFaint.Render("history: "+historyPath()),
		"",
		"  "+sFaint.Render("press any key to close"),
	)
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
	inDashboard = true
	_, err := tea.NewProgram(newModel(ps, every), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
