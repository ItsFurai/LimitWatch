# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- **Nine more providers:** GitHub Copilot (monthly quotas, through the GitHub CLI), Gemini CLI (daily quotas per model), Kimi Code (5-hour and weekly quotas), Z.ai / Zhipu GLM Coding Plan, the DeepSeek, Kimi (Moonshot) and OpenRouter balances, and OpenAI and Anthropic API spend this month.
- **Balances:** prepaid credit shows the amount left, a week of history, and how long it lasts at your spending over the last week. Top-ups don't count as spending.
- **API spend** can have a monthly budget (`limitwatch budget openai-api 100`), which turns it into a limit with a forecast; without one, limitwatch projects the month's total.
- **API keys** from environment variables or `limitwatch key set <provider>`, which asks for the key without echoing it and saves it readable only by you. `key list` and `key remove` manage them, and `key <provider>` does the same in the dashboard.
- `limitwatch providers` lists every provider and whether it's set up. Providers that aren't are hidden and named on one line.
- Counted quotas show their count, such as `79 / 2,000 completions`, and windows of a week or more show their pace per day.
- **A command bar** in the dashboard: press `:` or `/` to type `refresh`, `provider codex`, `every 30s`, `login claude`, `login codex`, `help` or `quit`, with tab completion and history. `!<command>` runs any shell command and comes back afterward.
- **Running apps:** each provider shows which of its apps are open (the Claude desktop app, Claude Code, Codex, the ChatGPT app), and a limit that can't be read comes with the next step to fix it, in the dashboard, `status` and `--json`.
- **Instant updates:** the dashboard refreshes within a couple of seconds when Claude Code refreshes its sign-in or Codex logs a new message.

### Changed
- When the live Codex check fails, a reading from Codex's log in the last 15 minutes is shown as current; older readings carry a warning that they may be out of date.
- Errors are shorter, and the steps to fix them moved to their own line.
- `--provider` also shows providers that aren't set up, with how to set them up.

## [0.1.0] - 2026-10-10

### Added
- **Live dashboard** with every limit of your Claude and ChatGPT (Codex) plans, refreshed every minute (`--every` to change it).
- **Forecasts:** for each limit, whether it will last until the reset (*on track*, *tight* or *runs out in ~40m*), the projected use at the reset with a range, and a *budget* pace that would last.
- **Pace from history:** readings are kept for 8 days, and the forecast uses your recent pace once there are enough of them. `limitwatch watch` records readings in the background.
- A timeline of each window so far, under its bar.
- **Claude:** session, weekly and per-model weekly limits, read with Claude Code's sign-in (file, macOS keychain or `CLAUDE_CODE_OAUTH_TOKEN`).
- **Codex:** both rate-limit windows, read with Codex's ChatGPT sign-in, falling back to Codex's session logs.
- `limitwatch status` (with `--json`) and `limitwatch statusline` for scripts, prompts and Claude Code's status line. The status line caches readings for a minute.
- `--provider` to pick providers, and `--demo` to try it with made-up numbers.
- Prebuilt downloads for Windows, macOS and Linux (x86-64 and ARM64) with checksums, and `go install` support.

[Unreleased]: https://github.com/ItsFurai/LimitWatch/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/ItsFurai/LimitWatch/releases/tag/v0.1.0
