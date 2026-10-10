package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"time"
)

const usage = `limitwatch — your AI plan limits, and whether they'll last until the reset

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
  limitwatch --version

OPTIONS (any command)
  --provider claude,codex     only these providers
  --demo                      show made-up numbers, to try it out

PROVIDERS
  claude   Claude Pro / Max, read from Claude Code's sign-in (~/.claude)
  codex    ChatGPT Plus / Pro, read from Codex's sign-in (~/.codex)

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
		fmt.Printf(usage, historyPath())
		return nil
	case "version":
		fmt.Println("limitwatch", version)
		return nil
	case "status":
		_, asJSON := flags["json"]
		return cmdStatus(ps, asJSON)
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
		return runTUI(ps, every)
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

func cmdStatus(ps []Provider, asJSON bool) error {
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
	fmt.Print(renderStatus(reports, hist, now, termWidth()))
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
