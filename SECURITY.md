# Security policy

## Supported versions

Security fixes go into the latest release. Please update to it before reporting.

## Reporting a vulnerability

Please **don't** open a public issue for security problems. Instead, use GitHub's private reporting:

1. Go to the [Security tab](https://github.com/ItsFurai/LimitWatch/security) of this repository.
2. Click **Report a vulnerability**.
3. Describe the problem, how to reproduce it, and what an attacker could do with it.

You should get a reply within a week. Once a fix is released, you'll be credited in the release notes unless you'd rather stay anonymous.

## Scope

limitwatch reads the sign-in tokens that Claude Code, Codex and Gemini CLI store on your machine, asks the GitHub CLI to make Copilot's request, and uses API keys from environment variables or its own config file. It sends each credential only to the provider it belongs to, over HTTPS, to read your usage. It never refreshes, logs or caches app sign-ins. API keys you save with `limitwatch key set` are stored in plain text in `config.json` in your user config folder, created readable only by your user, like most CLI tools' credential files; use environment variables instead if you'd rather not store them. Its other files, the usage cache and history, contain only percentages, amounts and reset times. Things worth reporting include any way limitwatch could leak a token (to another host, a log, an error message or its own files), or be tricked into sending one somewhere else.
