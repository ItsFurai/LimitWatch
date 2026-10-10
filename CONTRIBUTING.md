# Contributing to limitwatch

Thanks for helping out! Bug reports, ideas, docs fixes and code are all welcome.

## Reporting bugs and ideas

- **Bugs:** open a [bug report](https://github.com/ItsFurai/LimitWatch/issues/new?template=bug_report.yml). Include your OS, `limitwatch --version`, your plan, and what you saw. `limitwatch status --json` is safe to share; your tokens and credential files are not.
- **Ideas:** open a [feature request](https://github.com/ItsFurai/LimitWatch/issues/new?template=feature_request.yml) and describe the problem it solves, not just the solution.
- **Security issues:** please don't open a public issue; see [SECURITY.md](SECURITY.md).

For anything larger than a small fix, open an issue first so we can agree on the approach before you spend time on it.

## Development setup

You need Go 1.24 or newer. There are no C dependencies.

```bash
git clone https://github.com/ItsFurai/LimitWatch.git
cd LimitWatch
go build -o limitwatch ./cmd/limitwatch     # limitwatch.exe on Windows
go test ./...
```

To try a build without an account, or without touching your real history, use demo mode or point it at scratch files:

```bash
./limitwatch --demo
LIMITWATCH_DIR=/tmp/lw-test ./limitwatch status   # PowerShell: $env:LIMITWATCH_DIR = "..."
```

## Project layout

Everything lives in `cmd/limitwatch`:

| File | Responsibility |
|---|---|
| `main.go` | command-line commands and flags |
| `limits.go` | the `Limit` and `Report` types, fetching every provider, the cache |
| `claude.go` | Claude: reading Claude Code's sign-in and the usage endpoint |
| `codex.go` | Codex: reading Codex's sign-in, the usage endpoint and session logs |
| `procs.go`, `procs_*.go` | finding the Claude and ChatGPT apps that are running, and the hints based on them |
| `history.go` | recorded readings |
| `forecast.go` | paces, projections and verdicts |
| `render.go` | the limit rows, `status` and `statusline` output |
| `tui.go`, `style.go` | the dashboard, colors and help screen |
| `commands.go` | the command bar, `login`, shell commands, and watching for new readings |
| `demo.go` | made-up data for `--demo` and the screenshot |

### Adding a provider

Write a `fetchX(ctx) Report` that returns one `Limit` per usage window, with its percentage, window length and reset time, and add it to `providers` in `limits.go`. The forecast, history, dashboard and status line work from those fields, so nothing else needs to change. Return `errMissing` when the user isn't signed in, so the provider is shown dimmed rather than as an error, and add a parsing test with a sample reply.

## Commit messages

Commits follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/): a type, an optional scope, and a short summary in the imperative mood.

```
<type>(<optional scope>): <summary>

<optional body: what changed and why>
```

| Type | Use it for |
|---|---|
| `feat` | a new feature users will notice |
| `fix` | a bug fix |
| `perf` | a speed or memory improvement |
| `refactor` | a code change that doesn't change behavior |
| `test` | adding or fixing tests |
| `docs` | README, guides, changelog and other documentation |
| `build` | Go version, dependencies, build settings |
| `ci` | GitHub Actions workflows |
| `chore` | anything else, such as release prep: `chore(release): v1.2.3` |

Examples:

```
feat: add a Gemini provider
fix(codex): read reset times from newer session logs
docs: explain the budget pace in the README
```

Keep the summary under about 70 characters and don't end it with a period. Use the body to explain *why* when it isn't obvious. For a breaking change, add `!` after the type (`feat!: …`) and describe the change in the body.

## Before you open a pull request

CI runs these on Windows, macOS and Linux with Go 1.24 and the latest Go. Running them locally first saves a round trip:

```bash
gofmt -l .                       # should print nothing
go vet ./...
go test ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
```

Also:

- Add or update tests for behavior changes.
- Add a line to the **Unreleased** section of [CHANGELOG.md](CHANGELOG.md) for anything users will notice.
- Update the README and the `?` help screen if you change commands, keys or the forecast. The help screen lists commands from `commands` in `commands.go`.
- To refresh the README screenshot, run `LIMITWATCH_SCREENSHOT=../../docs go test -run TestScreenshot ./cmd/limitwatch` and then `freeze --language ansi docs/dashboard.ansi -o docs/screenshot.svg`.
- Keep pull requests focused: one change per PR is much easier to review.

## Releasing (maintainers)

1. Move the **Unreleased** entries in `CHANGELOG.md` under a new version heading and update the links at the bottom.
2. Commit, then tag and push: `git tag -a v1.2.3 -m "v1.2.3" && git push origin v1.2.3`.
3. The release workflow builds all platforms and publishes the release, using that version's changelog section as the notes.

## Code of conduct

Everyone taking part is expected to follow the [code of conduct](CODE_OF_CONDUCT.md).
