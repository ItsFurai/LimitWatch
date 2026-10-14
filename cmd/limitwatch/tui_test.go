package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
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

// TestScreenshot renders the dashboard with demo data and writes the raw
// ANSI frame for docs/. It only runs when asked:
//
//	LIMITWATCH_SCREENSHOT=../../docs go test -run TestScreenshot ./cmd/limitwatch
//	freeze --language ansi docs/dashboard.ansi -o docs/screenshot.svg
func TestScreenshot(t *testing.T) {
	out := os.Getenv("LIMITWATCH_SCREENSHOT")
	if out == "" {
		t.Skip("set LIMITWATCH_SCREENSHOT=<dir> to regenerate the README screenshot")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
	m := demoModel(t, 118, 80)
	m = feed(m, tea.WindowSizeMsg{Width: 118, Height: len(m.(model).bodyLines()) + 2})
	if err := os.WriteFile(filepath.Join(out, "dashboard.ansi"), []byte(m.View()), 0o644); err != nil {
		t.Fatal(err)
	}
}
