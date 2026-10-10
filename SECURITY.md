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

limitwatch reads the sign-in tokens that Claude Code and Codex store on your machine, and sends them only to the provider they belong to (`api.anthropic.com` and `chatgpt.com`) over HTTPS to read your usage. It never writes, refreshes, logs or caches those tokens; the only files it writes are its usage cache and history in your cache folder, which contain percentages and reset times. Things worth reporting include any way limitwatch could leak a token (to another host, a log, an error message or its own files), or be tricked into sending one somewhere else.
