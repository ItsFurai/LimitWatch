package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func feed(m tea.Model, msg tea.Msg) tea.Model {
	m, _ = m.Update(msg)
	return m
}

func demoModel(t *testing.T, w, h int) tea.Model {
	t.Helper()
	demoMode = true
	t.Cleanup(func() { demoMode = false })
	now := time.Now()
	reports := demoReports(now)
	var m tea.Model = newModel(providers, time.Minute)
	m = feed(m, tea.WindowSizeMsg{Width: w, Height: h})
	return feed(m, fetchedMsg{reports: reports, hist: demoHistory(reports, now)})
}

func TestViewFitsTheWindow(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {50, 10}} {
		m := demoModel(t, size[0], size[1])
		for _, view := range []string{m.View(), feed(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}).View()} {
			lines := strings.Split(view, "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > size[0] {
					t.Errorf("%dx%d: line %d is %d wide: %q", size[0], size[1], i, w, ansi.Strip(l))
				}
			}
		}
	}
}

func TestViewShowsVerdicts(t *testing.T) {
	view := ansi.Strip(demoModel(t, 130, 40).View())
	for _, want := range []string{"Claude", "Max 5x", "Codex", "runs out in", "tight", "on track", "resets in"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
}

func TestStatusline(t *testing.T) {
	now := time.Now()
	reports := demoReports(now)[:1]
	got := ansi.Strip(renderStatusline(reports, demoHistory(reports, now), now))
	if !strings.HasPrefix(got, "5h 78% ⚠") || !strings.Contains(got, "7d 42% ◐") {
		t.Fatalf("statusline %q", got)
	}
}
