# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

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
