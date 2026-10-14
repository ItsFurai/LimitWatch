package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
)

const usage = `limitwatch — your AI limits and balances, and whether they'll last

USAGE
  limitwatch                  open the live dashboard
      --every 1m                how often to refresh (minimum 30s)
  limitwatch status           print every limit and its forecast once
      --json                    machine-readable output
  limitwatch statusline       one short line for a shell prompt or Claude Code's status line
                                (cached for a minute, so it's cheap to call often)
  limitwatch watch [-v]       keep recording usage in the background, so forecasts
                                know your recent pace (Ctrl+C to stop)
      --every 5m                how often to record
  limitwatch providers        list every provider and whether it's set up
  limitwatch key set <p>      save an API key (asked for without echoing it)
  limitwatch key list         show which keys are set, and from where
  limitwatch key remove <p>   forget a saved key
  limitwatch budget <p> <n>   a monthly budget in USD for API spend (0 removes it)
  limitwatch --version

OPTIONS (any command)
  --provider claude,deepseek  only these providers (also shows them when not set up)
  --demo                      show made-up numbers, to try it out

PROVIDERS
%s
Providers that aren't set up are hidden; ` + "`limitwatch providers`" + ` lists them.
Config:  %s
History: %s  (override with LIMITWATCH_HISTORY)
`

// version is set at build time: -ldflags "-X main.version=v1.2.3"
var version = "dev"

var demoMode bool

func init() {
	// `go install …@v0.2.0` records the module version; use it when not stamped by a release build.
	if bi, ok := debug.ReadBuildInfo(); ok && version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		version = bi.Main.Version
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "limitwatch:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags, rest := splitFlags(args, map[string]bool{"provider": true, "every": true})
	if _, ok := flags["demo"]; ok || os.Getenv("LIMITWATCH_DEMO") != "" {
		demoMode = true
	}
	ps, err := selectProviders(flags["provider"])
	if err != nil {
		return err
	}
	if _, ok := flags["h"]; ok {
		rest = []string{"help"}
	}
	if _, ok := flags["help"]; ok {
		rest = []string{"help"}
	}
	if _, ok := flags["version"]; ok {
		rest = []string{"version"}
	}
	cmd := ""
	if len(rest) > 0 {
		cmd = rest[0]
	}
	switch cmd {
	case "help":
		fmt.Printf(usage, providerTable(), configPath(), historyPath())
		return nil
	case "providers":
		fmt.Print(providerTable())
		return nil
	case "key", "keys":
		return cmdKey(rest[1:])
	case "budget":
		if len(rest) != 3 {
			return fmt.Errorf("usage: limitwatch budget <%s> <amount in USD>", providerKeys(usesBudget))
		}
		if err := setBudget(rest[1], rest[2]); err != nil {
			return err
		}
		fmt.Println("saved; limitwatch status shows the forecast")
		return nil
	case "version":
		fmt.Println("limitwatch", version)
		return nil
	case "status":
		_, asJSON := flags["json"]
		return cmdStatus(ps, asJSON, flags["provider"] != "")
	case "statusline":
		return cmdStatusline(ps, flags["provider"] != "")
	case "watch":
		_, verbose := flags["v"]
		every, err := parseEvery(flags["every"], 5*time.Minute)
		if err != nil {
			return err
		}
		return cmdWatch(ps, every, verbose)
	case "":
		every, err := parseEvery(flags["every"], time.Minute)
		if err != nil {
			return err
		}
		return runTUI(ps, every, flags["provider"] != "")
	}
	return fmt.Errorf("unknown command %q — see `limitwatch --help`", cmd)
}

// splitFlags pulls -x / --x [value] flags out of args.
func splitFlags(args []string, withValue map[string]bool) (map[string]string, []string) {
	flags := map[string]string{}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			k, v, hasEq := strings.Cut(strings.TrimLeft(a, "-"), "=")
			if withValue[k] && !hasEq && i+1 < len(args) {
				i++
				v = args[i]
			}
			flags[k] = v
			continue
		}
		rest = append(rest, a)
	}
	return flags, rest
}

func parseEvery(s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("--every: %q isn't a duration like 30s, 1m or 5m", s)
	}
	// The usage endpoints are shared with the official apps; don't hammer them.
	return max(d, 30*time.Second), nil
}

type limitJSON struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Used          float64      `json:"used"`
	WindowSeconds int64        `json:"window_seconds"`
	ResetsAt      *time.Time   `json:"resets_at"`
	Forecast      forecastJSON `json:"forecast"`
}

type forecastJSON struct {
	Verdict     string     `json:"verdict"`
	SecondsLeft int64      `json:"seconds_left,omitempty"`
	AtReset     float64    `json:"projected_at_reset"`
	Low         float64    `json:"projected_low"`
	High        float64    `json:"projected_high"`
	AvgRate     float64    `json:"avg_rate_per_hour"`
	RecentRate  *float64   `json:"recent_rate_per_hour,omitempty"`
	SafeRate    float64    `json:"safe_rate_per_hour"`
	RunsOutAt   *time.Time `json:"runs_out_at,omitempty"`
}

// providerTable lists every provider, whether it's set up, and how.
func providerTable() string {
	var b strings.Builder
	cfg := loadConfig()
	for _, p := range providers {
		how := "app sign-in"
		if usesKey(p) {
			how = sFaint.Render("key: " + strings.Join(p.KeyEnv, " or "))
			if k, from := apiKey(p); k != "" {
				how = sOK.Render("✓ key from " + from)
			}
			if p.Budget && cfg.Budgets[p.Key] > 0 {
				how += sDim.Render(fmt.Sprintf(" · budget $%.0f", cfg.Budgets[p.Key]))
			}
		}
		fmt.Fprintf(&b, "  %s %s  %s\n", sKey.Render(fmt.Sprintf("%-14s", p.Key)), sDim.Render(fmt.Sprintf("%-66s", p.About)), how)
	}
	return b.String()
}

func cmdKey(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list", "ls":
		fmt.Print(keyList())
		return nil
	case "set", "add":
		if len(args) < 2 {
			return fmt.Errorf("usage: limitwatch key set <%s>", providerKeys(usesKey))
		}
		p, err := findProvider(args[1])
		if err != nil {
			return err
		}
		if !usesKey(p) {
			return fmt.Errorf("%s uses its app's sign-in, not an API key", p.Name)
		}
		key, err := readSecret(fmt.Sprintf("%s API key (%s): ", p.Name, strings.Join(p.KeyEnv, " also works")))
		if err != nil {
			return err
		}
		if err := setKey(p.Key, key); err != nil {
			return err
		}
		fmt.Printf("saved %s for %s in %s\n", maskKey(strings.TrimSpace(key)), p.Key, configPath())
		return nil
	case "remove", "rm", "delete":
		if len(args) < 2 {
			return errors.New("usage: limitwatch key remove <provider>")
		}
		if err := removeKey(args[1]); err != nil {
			return err
		}
		fmt.Println("removed")
		return nil
	}
	return fmt.Errorf("unknown key command %q (set, list or remove)", sub)
}

// readSecret asks for a key without echoing it. Piped input works too:
// echo $KEY | limitwatch key set deepseek
func readSecret(prompt string) (string, error) {
	if term.IsTerminal(os.Stdin.Fd()) {
		fmt.Fprint(os.Stderr, prompt)
		b, err := term.ReadPassword(os.Stdin.Fd())
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no key given")
	}
	return line, nil
}

// visible splits reports into those to show and the names of providers that
// aren't set up. A provider asked for by name is always shown.
func visible(reports []Report, explicit bool) ([]Report, []string) {
	if explicit {
		return reports, nil
	}
	var shown []Report
	var missing []string
	for _, r := range reports {
		if r.Missing {
			missing = append(missing, providerKeyOf(r.Provider))
			continue
		}
		shown = append(shown, r)
	}
	return shown, missing
}

func providerKeyOf(name string) string {
	for _, p := range providers {
		if p.Name == name {
			return p.Key
		}
	}
	return strings.ToLower(name)
}

// notSetUp is the line under the providers listing the hidden ones.
func notSetUp(missing []string) string {
	if len(missing) == 0 {
		return ""
	}
	how := "`limitwatch providers` shows how to add them"
	if inDashboard {
		how = "type `key <name>` or see `help`"
	}
	return sFaint.Render(" not set up: " + strings.Join(missing, ", ") + " · " + how)
}

func cmdStatus(ps []Provider, asJSON, explicit bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	reports := fetchAll(ctx, ps, 0)
	hist := historyFor(reports)
	now := time.Now()
	if asJSON {
		type reportJSON struct {
			Report
			Limits []limitJSON `json:"limits"`
		}
		var out []reportJSON
		for _, r := range reports {
			rj := reportJSON{Report: r, Limits: []limitJSON{}}
			for _, l := range r.Limits {
				f := Predict(l, hist.inWindow(l), now)
				fj := forecastJSON{Verdict: f.Verdict.String(), SecondsLeft: int64(f.Left.Seconds()),
					AtReset: round1(f.AtReset), Low: round1(f.Low), High: round1(f.High),
					AvgRate: round1(f.AvgRate), SafeRate: round1(f.SafeRate)}
				if f.HasRecent {
					r := round1(f.RecentRate)
					fj.RecentRate = &r
				}
				if f.Verdict == RunsOut {
					t := now.Add(f.EmptyIn).Round(time.Second)
					fj.RunsOutAt = &t
				}
				lj := limitJSON{ID: l.ID, Name: l.Name, Used: l.Used, WindowSeconds: int64(l.Window.Seconds()), Forecast: fj}
				if !l.ResetsAt.IsZero() {
					t := l.ResetsAt.Round(time.Second)
					lj.ResetsAt = &t
				}
				rj.Limits = append(rj.Limits, lj)
			}
			out = append(out, rj)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	shown, missing := visible(reports, explicit)
	fmt.Print(renderStatus(shown, hist, now, termWidth()))
	if line := notSetUp(missing); line != "" {
		fmt.Println("\n" + line)
	}
	return nil
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

// historyFor returns the saved history, or the made-up one in demo mode.
func historyFor(reports []Report) History {
	if demoMode {
		return demoHistory(reports, time.Now())
	}
	return loadHistory()
}

func cmdStatusline(ps []Provider, chosen bool) error {
	if !chosen {
		// Most status lines live inside Claude Code, so default to Claude alone.
		ps = ps[:1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	reports := fetchAll(ctx, ps, time.Minute)
	fmt.Println(renderStatusline(reports, historyFor(reports), time.Now()))
	return nil
}

func cmdWatch(ps []Provider, every time.Duration, verbose bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Fprintf(os.Stderr, "recording every %s to %s (Ctrl+C to stop)\n", every, historyPath())
	last := map[string]Verdict{}
	for {
		fctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		reports := fetchAll(fctx, ps, 0)
		cancel()
		hist := historyFor(reports)
		now := time.Now()
		for _, r := range reports {
			if r.Err != "" && !r.Missing && verbose {
				fmt.Fprintf(os.Stderr, "%s  %s: %s\n", now.Format("15:04"), r.Provider, r.Err)
			}
			for _, l := range r.Limits {
				f := Predict(l, hist.inWindow(l), now)
				prev, seen := last[l.ID]
				last[l.ID] = f.Verdict
				if verbose || (seen && prev != f.Verdict) || (!seen && f.Verdict >= Tight) {
					fmt.Fprintf(os.Stderr, "%s  %s %s  %s  %s\n", now.Format("15:04"), r.Provider, l.Name, pct(l.Used), verdictText(l, f))
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(every):
		}
	}
}
