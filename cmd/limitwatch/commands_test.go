package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name, path string
		want       appKind
	}{
		{"Claude.exe", `C:\Program Files\WindowsApps\Claude_2.31226.1.0_x64__pzs8sxrjxfjjc\app\Claude.exe`, appClaudeDesktop},
		{"claude.exe", `C:\Users\PC\AppData\Local\AnthropicClaude\app-0.9.3\claude.exe`, appClaudeDesktop},
		{"claude.exe", `C:\Users\PC\AppData\Roaming\Claude\claude-code\2.1.295\d2e29494ec83\claude.exe`, appClaudeCodeApp},
		{"claude.exe", `C:\Users\PC\.local\bin\claude.exe`, appClaudeCode},
		{"claude", "/Applications/Claude.app/Contents/MacOS/Claude", appClaudeDesktop},
		{"claude", "/Users/a/Library/Application Support/Claude/claude-code/2.1/claude", appClaudeCodeApp},
		{"claude", "/home/a/.local/bin/claude", appClaudeCode},
		{"codex.exe", `C:\Users\PC\AppData\Roaming\npm\node_modules\@openai\codex\bin\codex.exe`, appCodex},
		{"codex-x86_64-pc-windows-msvc.exe", "", appCodex},
		{"ChatGPT.exe", `C:\Program Files\WindowsApps\OpenAI.ChatGPT\app\ChatGPT.exe`, appChatGPT},
		{"claudette.exe", "", appNone},
		{"node.exe", "", appNone},
	}
	for _, c := range cases {
		if got := classify(proc{Name: c.name, Path: c.path}); got != c.want {
			t.Errorf("classify(%s, %s) = %v, want %v", c.name, c.path, got, c.want)
		}
	}
}

func TestHints(t *testing.T) {
	failed := Report{Provider: "Claude", Err: "sign-in expired"}
	desktop := Running{appClaudeDesktop: 1, appClaudeCodeApp: 2}
	if h := desktop.hint(failed, true); !strings.Contains(h, "desktop app keeps its sign-in") || !strings.Contains(h, "type `login claude`") {
		t.Errorf("desktop, dashboard: %q", h)
	}
	if h := desktop.hint(failed, false); !strings.Contains(h, "run `claude`") {
		t.Errorf("desktop, shell: %q", h)
	}
	if h := (Running{appClaudeCode: 1}).hint(failed, true); !strings.Contains(h, "Claude Code is running") {
		t.Errorf("CLI running: %q", h)
	}
	if h := (Running{}).hint(Report{Provider: "Codex", Missing: true, Err: "x"}, false); h != "Run `codex login` to sign in." {
		t.Errorf("codex, nothing running: %q", h)
	}
	if h := desktop.hint(Report{Provider: "Claude"}, true); h != "" {
		t.Errorf("hint for a working provider: %q", h)
	}
	if got := strings.Join(desktop.labels("Claude"), ","); got != "desktop app" {
		t.Errorf("labels %q", got)
	}
}

func TestCodexFallbackFreshness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	write := func(age time.Duration) {
		now := time.Now()
		dir := filepath.Join(home, "sessions", now.Format("2006/01/02"))
		os.MkdirAll(dir, 0o755)
		ts := now.Add(-age).UTC().Format(time.RFC3339Nano)
		line := `{"timestamp":"` + ts + `","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":40,"window_minutes":300,"resets_in_seconds":14000}}}}` + "\n"
		os.WriteFile(filepath.Join(dir, "rollout-x.jsonl"), []byte(line), 0o644)
	}
	write(2 * time.Minute)
	if r := codexFallback(os.ErrDeadlineExceeded); r.Err != "" || len(r.Limits) != 1 || !strings.Contains(r.Source, "2m ago") {
		t.Fatalf("fresh log: %+v", r)
	}
	write(time.Hour)
	if r := codexFallback(os.ErrDeadlineExceeded); !strings.Contains(r.Err, "may be out of date") || len(r.Limits) != 1 {
		t.Fatalf("old log: %+v", r)
	}
	os.RemoveAll(filepath.Join(home, "sessions"))
	if r := codexFallback(errMissing{"not signed in"}); !r.Missing {
		t.Fatalf("no log: %+v", r)
	}
}

func typeLine(m tea.Model, s string) tea.Model {
	m = feed(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	for _, r := range s {
		m = feed(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestCommandBar(t *testing.T) {
	m := demoModel(t, 120, 30)

	m = typeLine(m, "prov")
	if !m.(model).cmdOn || !strings.Contains(ansi.Strip(m.View()), "provider all") {
		t.Fatalf("bar not showing suggestions:\n%s", ansi.Strip(m.View()))
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyTab})
	if got := m.(model).input.Value(); got != "provider all" {
		t.Fatalf("tab completed to %q", got)
	}
	// Replace the completion with a specific provider and run it.
	for range "all" {
		m = feed(m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	for _, r := range "codex" {
		m = feed(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEnter})
	mm := m.(model)
	if mm.cmdOn || len(mm.ps) != 1 || mm.ps[0].Key != "codex" || len(mm.reports) != 1 || mm.reports[0].Provider != "Codex" {
		t.Fatalf("provider codex: on=%v ps=%v reports=%d", mm.cmdOn, mm.ps, len(mm.reports))
	}

	m = feed(typeLine(m, "every 2m"), tea.KeyMsg{Type: tea.KeyEnter})
	if m.(model).every != 2*time.Minute {
		t.Fatalf("every: %v", m.(model).every)
	}
	m = feed(typeLine(m, "every 5s"), tea.KeyMsg{Type: tea.KeyEnter})
	if m.(model).every != 30*time.Second {
		t.Fatalf("every is clamped to 30s, got %v", m.(model).every)
	}

	m = feed(typeLine(m, "frobnicate"), tea.KeyMsg{Type: tea.KeyEnter})
	if mm := m.(model); !mm.flashErr || !strings.Contains(mm.flash, "unknown command") {
		t.Fatalf("unknown command flash: %q", mm.flash)
	}

	// History: ↑ brings back the last command.
	m = typeLine(m, "")
	m = feed(m, tea.KeyMsg{Type: tea.KeyUp})
	if got := m.(model).input.Value(); got != "frobnicate" {
		t.Fatalf("history ↑ = %q", got)
	}
	m = feed(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.(model).cmdOn {
		t.Fatal("esc didn't close the bar")
	}

	// q types into the bar instead of quitting.
	m = typeLine(m, "q")
	if got := m.(model).input.Value(); got != "q" {
		t.Fatalf("typed %q", got)
	}
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("quit command returned no tea.Quit")
	}

	// ! opens the bar ready for a shell command.
	m = feed(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}})
	if got := m.(model).input.Value(); got != "!" {
		t.Fatalf("! opened the bar with %q", got)
	}
}

func TestSuggestionsAndComplete(t *testing.T) {
	if got := strings.Join(suggestions(""), " "); got != "refresh provider every login key budget !<command> help quit" {
		t.Errorf("suggestions(\"\") = %q", got)
	}
	if got := strings.Join(suggestions("login"), ","); got != "login claude,login codex,login gemini,login copilot" {
		t.Errorf("suggestions(login) = %q", got)
	}
	if got := complete("login co"); got != "login codex" {
		t.Errorf("complete = %q", got)
	}
	if got := complete("!ls"); got != "!ls" {
		t.Errorf("complete shell = %q", got)
	}
}

func TestPausedShellCommand(t *testing.T) {
	var out strings.Builder
	p := &pausedCmd{cmd: shellCommand("echo limitwatch-ok"), label: "echo"}
	p.SetStdin(strings.NewReader("\n"))
	p.SetStdout(&out)
	p.SetStderr(&out)
	if err := p.Run(); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	got := ansi.Strip(out.String())
	if !strings.Contains(got, "limitwatch-ok") || !strings.Contains(got, "✓ done") || !strings.Contains(got, "press Enter") {
		t.Fatalf("output:\n%s", got)
	}

	out.Reset()
	p = &pausedCmd{cmd: shellCommand("exit 3"), label: "fail"}
	p.SetStdin(strings.NewReader("\n"))
	p.SetStdout(&out)
	p.SetStderr(&out)
	if err := p.Run(); err == nil || !strings.Contains(ansi.Strip(out.String()), "exited with code 3") {
		t.Fatalf("err=%v output:\n%s", err, out.String())
	}
}
