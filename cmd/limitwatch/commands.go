package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The command bar opens with `:` or `/` (or `!` for a shell command) and
// runs one of these. The list also drives tab completion and the help screen.
var commands = []struct{ name, desc string }{
	{"refresh", "check every limit now (also: r)"},
	{"provider all", "show every provider"},
	{"provider claude", "show only Claude (or codex, or claude,codex)"},
	{"every 30s", "refresh every 30s (minimum), 1m, 5m, …"},
	{"login claude", "open Claude Code here to refresh its sign-in"},
	{"login codex", "run `codex login` here"},
	{"login gemini", "open Gemini CLI here to refresh its sign-in"},
	{"login copilot", "run `gh auth login` here"},
	{"key <provider>", "add an API key: deepseek, kimi, moonshot, zai, openrouter, openai-api, anthropic-api"},
	{"budget <provider> <usd>", "a monthly budget for openai-api or anthropic-api (0 removes it)"},
	{"!<command>", "run a shell command, then come back"},
	{"help", "this screen"},
	{"quit", "leave limitwatch (also: q, exit)"},
}

type execDoneMsg struct {
	label string
	err   error
}

// runCommand carries out one line typed into the command bar.
func (m model) runCommand(line string) (model, tea.Cmd) {
	line = strings.TrimSpace(line)
	if line == "" {
		return m, nil
	}
	if strings.HasPrefix(line, "!") {
		sh := strings.TrimSpace(line[1:])
		if sh == "" {
			return m, m.setFlash("type a shell command after the !", true)
		}
		return m, runPaused(sh, shellCommand(sh))
	}
	fields := strings.Fields(strings.ToLower(line))
	arg := strings.Join(fields[1:], ",")
	switch fields[0] {
	case "r", "refresh", "reload":
		return m.refresh()
	case "q", "quit", "exit":
		return m, tea.Quit
	case "help", "?", "h":
		m.showHelp = true
		return m, nil
	case "provider", "providers", "p":
		if arg == "" || arg == "all" {
			arg = ""
		}
		ps, err := selectProviders(arg)
		if err != nil {
			return m, m.setFlash(err.Error(), true)
		}
		m.ps, m.explicit = ps, arg != ""
		m.reports = filterReports(m.reports, ps)
		return m.refresh()
	case "every":
		d, err := parseEvery(arg, 0)
		if err != nil || d == 0 {
			return m, m.setFlash("every needs a duration, such as every 30s or every 5m", true)
		}
		m.every = d
		return m, m.setFlash("refreshing every "+humanDur(d), false)
	case "login":
		return m.login(arg)
	case "key", "keys":
		if len(fields) < 2 {
			return m, m.setFlash("key for which provider? "+providerKeys(usesKey), true)
		}
		p, err := findProvider(fields[1])
		if err != nil {
			return m, m.setFlash(err.Error(), true)
		}
		if !usesKey(p) {
			return m, m.setFlash(p.Name+" uses its app's sign-in; try login "+p.Key, true)
		}
		if len(fields) > 2 {
			return m, m.setFlash("type just `key "+p.Key+"`; the key is asked for next, hidden", true)
		}
		return m, m.askKey(p)
	case "budget":
		if len(fields) != 3 {
			return m, m.setFlash("budget needs a provider and an amount, such as budget openai-api 100", true)
		}
		if err := setBudget(fields[1], fields[2]); err != nil {
			return m, m.setFlash(err.Error(), true)
		}
		m.setFlash("budget saved", false)
		return m.refresh()
	}
	return m, m.setFlash(fmt.Sprintf("unknown command %q; type help to see them all", fields[0]), true)
}

func (m model) refresh() (model, tea.Cmd) {
	if m.fetching {
		return m, nil
	}
	m.fetching = true
	return m, m.fetch(true)
}

func filterReports(reports []Report, ps []Provider) []Report {
	var out []Report
	for _, r := range reports {
		for _, p := range ps {
			if p.Name == r.Provider {
				out = append(out, r)
			}
		}
	}
	return out
}

// login hands the terminal to the provider's own CLI, which refreshes or
// redoes its sign-in; limitwatch then reads the new one.
func (m model) login(which string) (model, tea.Cmd) {
	var bin string
	var args []string
	switch which {
	case "claude":
		bin = "claude"
	case "codex":
		bin, args = "codex", []string{"login"}
	case "gemini":
		bin = "gemini"
	case "copilot", "github", "gh":
		bin, args = "gh", []string{"auth", "login"}
	default:
		return m, m.setFlash("login what? claude, codex, gemini or copilot", true)
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		install := map[string]string{
			"claude": "Claude Code isn't on your PATH; install it from claude.com/claude-code",
			"codex":  "Codex isn't on your PATH; install it with npm install -g @openai/codex",
			"gemini": "Gemini CLI isn't on your PATH; install it with npm install -g @google/gemini-cli",
			"gh":     "the GitHub CLI isn't on your PATH; install it from cli.github.com",
		}
		return m, m.setFlash(install[bin], true)
	}
	label := strings.Join(append([]string{bin}, args...), " ")
	if which == "claude" || which == "gemini" {
		label += "  (it refreshes its sign-in on start; quit it with /exit to come back)"
	}
	return m, runPaused(label, exec.Command(path, args...))
}

func shellCommand(line string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("powershell.exe", "-NoLogo", "-Command", line)
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return exec.Command(sh, "-c", line)
}

// runPaused leaves the dashboard, runs cmd in the terminal, waits for Enter
// so its output can be read, then comes back and refreshes.
func runPaused(label string, cmd *exec.Cmd) tea.Cmd {
	return tea.Exec(&pausedCmd{cmd: cmd, label: label}, func(err error) tea.Msg {
		return execDoneMsg{label: label, err: err}
	})
}

type pausedCmd struct {
	cmd    *exec.Cmd
	label  string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func (p *pausedCmd) SetStdin(r io.Reader)  { p.stdin = r }
func (p *pausedCmd) SetStdout(w io.Writer) { p.stdout = w }
func (p *pausedCmd) SetStderr(w io.Writer) { p.stderr = w }

func (p *pausedCmd) Run() error {
	in, out, errOut := p.stdin, p.stdout, p.stderr
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}
	p.cmd.Stdin, p.cmd.Stdout, p.cmd.Stderr = in, out, errOut
	fmt.Fprintln(out, sKey.Render("▶ ")+sBold.Render(p.label)+"\n")
	start := time.Now()
	err := p.cmd.Run()
	status := sOK.Render("✓ done")
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		status = sErr.Render(fmt.Sprintf("✕ exited with code %d", exitErr.ExitCode()))
	case err != nil:
		status = sErr.Render("✕ " + err.Error())
	}
	fmt.Fprintf(out, "\n%s %s  %s", status, sDim.Render("in "+humanDurShort(time.Since(start))),
		sFaint.Render("press Enter to return to limitwatch"))
	bufio.NewReader(in).ReadString('\n')
	return err
}

func humanDurShort(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return humanDur(d)
}

// complete finishes a partly typed command from the list above.
func complete(line string) string {
	l := strings.ToLower(strings.TrimLeft(line, " "))
	if l == "" || strings.HasPrefix(l, "!") {
		return line
	}
	for _, c := range commands {
		if strings.HasPrefix(c.name, l) && !strings.ContainsAny(c.name, "<") {
			return c.name
		}
	}
	return line
}

// suggestions lists the commands that start with what's been typed.
func suggestions(line string) []string {
	l := strings.ToLower(strings.TrimLeft(line, " "))
	var out []string
	for _, c := range commands {
		name := c.name
		if l == "" || strings.Contains(name, "<") {
			name = strings.Fields(name)[0] // just the verbs until something is typed
		}
		if strings.HasPrefix(name, l) && !contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

// sourceStamps changes when Claude Code rewrites its sign-in or Codex logs a
// message, so the dashboard can pick up new numbers right away.
func sourceStamps() (claude, codex time.Time) {
	if st, err := os.Stat(filepath.Join(claudeConfigDir(), ".credentials.json")); err == nil {
		claude = st.ModTime()
	}
	if files := recentRollouts(filepath.Join(codexHome(), "sessions"), time.Now(), 2); len(files) > 0 {
		if st, err := os.Stat(files[0]); err == nil {
			codex = st.ModTime()
		}
	}
	return claude, codex
}
