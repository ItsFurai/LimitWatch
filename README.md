<div align="center">

# limitwatch

**Will your AI limits last until the reset?**
See your Claude, ChatGPT (Codex), GitHub Copilot, Gemini, Kimi, Z.ai, DeepSeek, OpenRouter and API spend limits in one terminal dashboard, with a forecast of whether you'll run out before they reset, and how long your prepaid credit will last.

[![CI](https://github.com/ItsFurai/LimitWatch/actions/workflows/ci.yml/badge.svg)](https://github.com/ItsFurai/LimitWatch/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ItsFurai/LimitWatch)](https://github.com/ItsFurai/LimitWatch/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/ItsFurai/LimitWatch)](go.mod)
[![License: MIT](https://img.shields.io/github/license/ItsFurai/LimitWatch)](LICENSE)

<img src="docs/screenshot.svg" alt="limitwatch showing a Claude session limit that will run out in 39 minutes, a tight weekly limit, Codex limits that are on track, a Copilot premium quota that runs out before the month ends, and a DeepSeek balance that lasts about ten days" width="900">

[Install](#install) · [Providers](#providers) · [Usage](#usage) · [Reading the dashboard](#reading-the-dashboard) · [Commands](#commands) · [How the forecast works](#how-the-forecast-works) · [Where the numbers come from](#where-the-numbers-come-from) · [Contributing](CONTRIBUTING.md) · [Changelog](CHANGELOG.md)

</div>

## Features

- **Every limit in one place.** [11 providers](#providers): subscription quotas (Claude, Codex, Copilot, Gemini CLI, Kimi Code, Z.ai), prepaid balances (DeepSeek, Kimi API, OpenRouter) and API spend (OpenAI, Anthropic), side by side.
- **A forecast, not just a percentage.** For each limit you get a verdict, *on track*, *tight* or *runs out in ~40m*, plus how much will be left at the reset.
- **Knows your pace.** It compares your average pace since the window started with your pace over the last stretch, and shows the range between them.
- **Tells you how to make it.** A *budget* pace for each limit: stay under it and you'll last until the reset.
- **Live dashboard.** Refreshes every minute, with a timeline of how each window has gone so far, and right away when Claude Code refreshes its sign-in or Codex logs a new message.
- **Knows what's running.** Shows which Claude and ChatGPT apps are open, and when a limit can't be read, tells you exactly what to do about it.
- **A command bar.** Switch providers, change the refresh rate, sign in again or run any shell command without leaving the dashboard.
- **Scriptable.** `limitwatch status --json` for scripts, and `limitwatch statusline` for a shell prompt or Claude Code's status line.
- **Little to configure.** It reads the sign-ins Claude Code, Codex, the GitHub CLI and Gemini CLI already have, and takes API keys from environment variables or `limitwatch key set`. It never refreshes sign-ins, and sends each credential only to its own provider.
- **Balances too.** For prepaid credit it measures what you spent over the last week, ignoring top-ups, and tells you how many days are left.
- **One small binary.** About 8 MB, no runtime or C dependencies, for Windows, macOS and Linux on x86-64 and ARM64.

## Install

### Download a release

Get a build for your system from the [latest release](https://github.com/ItsFurai/LimitWatch/releases/latest), unzip it, and put the folder on your `PATH`. Each release includes `checksums.txt` to verify the download.

On macOS, the builds aren't code-signed, so clear the quarantine flag once:

```bash
xattr -d com.apple.quarantine limitwatch
```

### With Go

Needs Go 1.24 or newer:

```bash
go install github.com/ItsFurai/LimitWatch/cmd/limitwatch@latest
```

### From source

```bash
git clone https://github.com/ItsFurai/LimitWatch.git
cd LimitWatch
go build -o limitwatch ./cmd/limitwatch
```

On Windows, use `-o limitwatch.exe`.

## Providers

| Provider | What it shows | How it signs in |
|---|---|---|
| `claude` | Claude Pro / Max: 5-hour session, weekly, and per-model weekly limits | [Claude Code](https://docs.anthropic.com/en/docs/claude-code)'s sign-in |
| `codex` | ChatGPT Plus / Pro / Team limits for [Codex](https://github.com/openai/codex) | Codex's sign-in, or its session logs |
| `copilot` | GitHub Copilot monthly premium requests, chat and completions | the [GitHub CLI](https://cli.github.com)'s sign-in (`gh auth login`), or `GH_TOKEN` |
| `gemini` | Gemini CLI / Code Assist daily quotas per model | [Gemini CLI](https://github.com/google-gemini/gemini-cli)'s Google sign-in |
| `kimi` | Kimi Code membership: 5-hour window and weekly quota | `KIMI_CODE_API_KEY` (a `sk-kimi-…` key from the Kimi Code console) |
| `zai` | Z.ai / Zhipu GLM Coding Plan: 5-hour tokens and monthly tool calls | `ZAI_API_KEY` (api.z.ai) or `ZHIPU_API_KEY` (bigmodel.cn) |
| `deepseek` | DeepSeek API balance | `DEEPSEEK_API_KEY` |
| `moonshot` | Kimi (Moonshot) API platform balance, on .ai or .cn | `MOONSHOT_API_KEY` |
| `openrouter` | OpenRouter credits, and the key's spending cap if it has one | `OPENROUTER_API_KEY` |
| `openai-api` | OpenAI API spend this month, against a budget you set | `OPENAI_ADMIN_KEY` (an `sk-admin-…` key) |
| `anthropic-api` | Claude API spend this month, against a budget you set | `ANTHROPIC_ADMIN_KEY` (an `sk-ant-admin…` key) |

Providers that aren't set up are hidden, and listed on one line at the bottom. `limitwatch providers` shows which are ready.

### API keys

Set a key as an environment variable, or save it once:

```bash
limitwatch key set deepseek
```

limitwatch asks for the key without showing it as you type, and saves it to `config.json` in your user config folder (`%APPDATA%\limitwatch` on Windows), readable only by you. In the dashboard, the command `key deepseek` does the same. An environment variable always wins over a saved key. `limitwatch key list` shows which keys are set and where they come from, and `limitwatch key remove deepseek` forgets one.

### Budgets for API spend

API spend has no limit of its own, so give it one:

```bash
limitwatch budget openai-api 100
```

Then the month's spend gets a bar and a verdict like any other limit, such as *over budget in ~4d*. Without a budget, limitwatch shows what you've spent and projects the month's total.

### Not supported

Some services have no usage data to read: Gemini **API keys**, Mistral, Groq, Perplexity, xAI and Cursor don't offer an endpoint for remaining quota or balance with a normal key, and the ChatGPT and Claude desktop apps keep theirs to themselves. If that changes, or you know of one, please [open an issue](https://github.com/ItsFurai/LimitWatch/issues/new?template=feature_request.yml).

## Usage

Sign in to any of the apps above, or add an API key, and run `limitwatch`.

```bash
limitwatch                       # open the live dashboard
limitwatch status                # print every limit and its forecast once
limitwatch status --json         # the same, for scripts
limitwatch statusline            # one short line: 5h 78% ⚠39m · 7d 42% ◐
limitwatch watch                 # record usage in the background, for better forecasts
limitwatch --provider claude     # only Claude (or a list: claude,deepseek)
limitwatch providers             # every provider, and whether it's set up
limitwatch key set kimi          # save an API key
limitwatch budget openai-api 100 # a monthly budget for API spend
limitwatch --demo                # made-up numbers, to try it without an account
```

### In Claude Code's status line

Add this to `~/.claude/settings.json`:

```json
{
  "statusLine": { "type": "command", "command": "limitwatch statusline" }
}
```

The status line caches its reading for a minute, so it's cheap to call on every message.

## Reading the dashboard

Each limit takes two lines:

```
 Session      ████████████████████▒▒▒▒▒▒   78%  ⚠ runs out in ~39m · 1h 1m before reset
              ▁▁▁▁▂▂▂▃▃▃▄▄▄▅▅▆▆▆········        resets in 1h 40m (21:30) · 34%/h now, 23%/h avg · budget 13%/h
```

| Part | Meaning |
|---|---|
| `█` | used so far |
| `▒` | how much more you're projected to use before the reset |
| `░` | what's still free after that |
| `▁▂▃▅▇` | how usage grew through the window so far (from recorded readings) |
| `···` | the time left until the reset |
| `34%/h now` | your pace over the last stretch |
| `23%/h avg` | your pace since the window started |
| `budget 13%/h` | the fastest pace that still lasts until the reset |

Copilot, Z.ai and OpenRouter count in requests or dollars, so their first line also shows the count, such as `79 / 2,000 completions`.

A **balance** looks a little different: the number is what's left, the bar is how much of the week's highest balance remains, the line under it is the last week of the balance, and the verdict says how long it lasts at your spending over that week:

```
 Balance      ████████████████░░░░░░░   $12.40  ◐ lasts ~9d 17h
              ▅▅▅▅▅▅▄▄▄████▇▇▇▇▇▇▆▆▆▆           spending $1.28/day over the last week
```

A balance is red when it runs out within 3 days and orange within 14.

The bar and verdict are colored by the forecast:

| Verdict | Color | Meaning |
|---|---|---|
| `✓ on track` | green | projected to stay under 90% by the reset |
| `◐ tight` | orange | projected to land between 90% and 100% |
| `⚠ runs out` | red | projected to hit 100% before the reset, and how early |
| `✕ limit reached` | red | already at 100%, and when it comes back |
| `○ idle` | blue | the window hasn't started, or nothing is used yet |

Each box's title also shows which of the provider's apps are running, such as `● desktop app, Claude Code`. When a limit can't be read, the line starting with `→` says what to do next.

### Keys

| Key | Action |
|---|---|
| `:` · `/` | open the command bar |
| `!` | open the command bar for a shell command |
| `r` | refresh now |
| `↑` `↓` · wheel | scroll |
| `?` | help |
| `q` · `esc` | quit |

## Commands

Press `:` or `/`, type a command and press `enter`. `tab` completes it, `↑` `↓` go through the ones you've used, and `esc` closes the bar.

| Command | Action |
|---|---|
| `refresh` | check every limit now |
| `provider claude` · `provider claude,deepseek` · `provider all` | choose which providers to show |
| `every 30s` | change how often it refreshes (30 seconds at the least) |
| `login claude` | open Claude Code here, which refreshes its sign-in; `/exit` brings you back |
| `login codex` | run `codex login` here |
| `login gemini` · `login copilot` | open Gemini CLI, or run `gh auth login` |
| `key deepseek` | add or replace an API key; it's typed hidden and never saved in the command history |
| `budget openai-api 100` | set a monthly budget for API spend (`0` removes it) |
| `!<command>` | run any shell command, such as `!codex login --help`; press `enter` afterward to come back |
| `help` · `quit` | show the help screen · leave |

Shell commands run in PowerShell on Windows and in your `$SHELL` elsewhere. When one finishes, limitwatch refreshes, so a new sign-in shows up right away.

## How the forecast works

limitwatch uses two paces for every limit:

- **Average pace:** the percentage used, divided by the time since the window started. Windows have a fixed length (5 hours, 7 days) and a known reset time, so this works from the very first reading.
- **Recent pace:** a straight-line fit through your readings over the last fifth of the window: the last hour of a 5-hour window, or about a day and a half of a week. It needs readings covering at least a sixth of that stretch.

The verdict uses the recent pace when it's known, because it reflects how you're working right now, and falls back to the average otherwise. The range in brackets, such as `~91% at reset (77–91%)`, is the projection at the slower and the faster of the two paces.

Readings come from every refresh and are kept for 8 days in `history.jsonl` in your cache folder (`%LOCALAPPDATA%\limitwatch` on Windows; override it with `LIMITWATCH_HISTORY`). Keep the dashboard open, or run `limitwatch watch` in the background, so the recent pace is always available.

Usage comes in bursts, so treat the forecast as a guide: it tells you whether your current pace is sustainable, not what you'll actually do.

## Where the numbers come from

Subscription limits have no public API, so for those limitwatch reads the same sources the providers' own apps use. Balances and spend come from documented APIs.

| Provider | Source |
|---|---|
| **Claude** | The usage endpoint behind Claude Code's `/usage` screen, signed in with the token Claude Code saved in `~/.claude/.credentials.json` (the login keychain on macOS). Set `CLAUDE_CONFIG_DIR` if yours lives elsewhere, or `CLAUDE_CODE_OAUTH_TOKEN` to use a token from `claude setup-token`. |
| **Codex** | The usage endpoint behind Codex's `/status`, signed in with `~/.codex/auth.json` (`CODEX_HOME` to override). If that fails, it falls back to the latest rate-limit snapshot in Codex's session logs, which is as fresh as your last Codex message. |
| **Copilot** | The quota endpoint Copilot's editor extensions read, called through `gh api copilot_internal/user`, so limitwatch never handles your GitHub token. |
| **Gemini** | The Code Assist quota endpoint Gemini CLI uses, signed in with `~/.gemini/oauth_creds.json` (`GEMINI_CLI_HOME` to override; `GOOGLE_CLOUD_PROJECT` to pick the project). Only Google-account sign-ins have these quotas. |
| **Kimi Code** | `api.kimi.com/coding/v1/usages`, which the Kimi Code console reads. |
| **Z.ai** | The quota monitor Z.ai's own plan-usage plugin reads (`ZAI_BASE_URL` to override). |
| **DeepSeek, Kimi API, OpenRouter** | Their documented balance and key endpoints. |
| **OpenAI API, Anthropic API** | Their documented cost-report admin APIs, totalled from the 1st of the month (UTC). |

limitwatch only **reads** these sign-ins. It never refreshes them, so if one has expired it tells you what to do, usually `login claude` or `login codex` in the command bar.

### Apps that are running

limitwatch checks which apps are open, by process name and path, and uses that to help:

| Running | What happens |
|---|---|
| **Claude Code** in a terminal or editor | It refreshes its sign-in as it works, and limitwatch notices the change and refreshes within a couple of seconds. |
| **Codex** | It logs your current limits after every message. limitwatch refreshes as soon as a new one is written, and uses it when the live check fails. A reading from the last 15 minutes counts as current; older ones are shown with a warning. |
| **Claude desktop app** | It keeps its sign-in to itself, encrypted, so limitwatch can't use it. Run `login claude` now and then to keep Claude Code's sign-in fresh. |
| **ChatGPT desktop app** | It doesn't share its sign-in or its message limits either. |

limitwatch never reads other apps' memory or their encrypted storage.

The subscription endpoints are unofficial and may change. If a provider changes its reply, limitwatch shows an error for that provider rather than wrong numbers. Please [open an issue](https://github.com/ItsFurai/LimitWatch/issues/new?template=bug_report.yml).

## Platform notes

- **Windows, Linux:** sign-ins are read from the files above.
- **macOS:** Claude Code's sign-in is read from the login keychain with `security`; macOS may ask once to allow it.
- Colors adapt to light and dark terminals.

## Contributing

Bug reports, ideas and pull requests are welcome, especially support for more providers. See [CONTRIBUTING.md](CONTRIBUTING.md) for how to build, test and submit changes, and [SECURITY.md](SECURITY.md) to report a vulnerability privately.

## License

[MIT](LICENSE) © 2026 Basel Elgamal
