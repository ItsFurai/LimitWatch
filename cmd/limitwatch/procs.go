package main

import (
	"path/filepath"
	"strings"
)

// proc is a running process: its executable name and, when the OS lets us
// read it, its full path.
type proc struct {
	PID  int
	Name string
	Path string
}

type appKind int

const (
	appNone          appKind = iota
	appClaudeDesktop         // the Claude desktop app
	appClaudeCodeApp         // Claude Code run by the desktop app (its Code tab)
	appClaudeCode            // Claude Code in a terminal or editor
	appCodex                 // the Codex CLI, editor extension or app
	appChatGPT               // the ChatGPT desktop app
)

// classify tells the apps apart by executable name and path. The desktop app
// and Claude Code are both "claude.exe" on Windows; the desktop app lives in
// its install folder and runs its own copy of Claude Code from its data folder.
func classify(p proc) appKind {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(p.Name), filepath.Ext(p.Name)))
	path := strings.ToLower(filepath.ToSlash(p.Path))
	switch {
	case name == "claude":
		switch {
		case strings.Contains(path, "/claude/claude-code/"), strings.Contains(path, "/application support/claude/claude-code/"):
			return appClaudeCodeApp
		case strings.Contains(path, "/windowsapps/claude_"), strings.Contains(path, "/anthropicclaude/"),
			strings.Contains(path, "/claude.app/contents/macos/"):
			return appClaudeDesktop
		default:
			return appClaudeCode
		}
	case name == "codex" || strings.HasPrefix(name, "codex-"):
		return appCodex
	case name == "chatgpt":
		return appChatGPT
	}
	return appNone
}

// Running is which AI apps are open right now.
type Running map[appKind]int // kind → number of processes

func detectRunning() Running {
	r := Running{}
	for _, p := range listProcs() {
		if k := classify(p); k != appNone {
			r[k]++
		}
	}
	return r
}

// runningLabels names the apps for one provider, for its box title.
func (r Running) labels(provider string) []string {
	var out []string
	add := func(k appKind, label string) {
		if r[k] > 0 {
			out = append(out, label)
		}
	}
	switch provider {
	case "Claude":
		add(appClaudeDesktop, "desktop app")
		add(appClaudeCode, "Claude Code")
	case "Codex":
		add(appCodex, "Codex")
		add(appChatGPT, "ChatGPT app")
	}
	return out
}

// hint is the next step for a provider whose limits couldn't be read,
// based on what's running. In the dashboard the step is a command typed
// there; elsewhere it's a shell command.
func (r Running) hint(rep Report, tui bool) string {
	if rep.Err == "" && !rep.Missing {
		return ""
	}
	login := func(provider, shell string) string {
		if tui {
			return "type `login " + provider + "`"
		}
		return "run `" + shell + "`"
	}
	claude, codex := login("claude", "claude"), login("codex", "codex login")
	switch rep.Provider {
	case "Claude":
		switch {
		case r[appClaudeCode] > 0:
			return "Claude Code is running; limitwatch picks up its sign-in as soon as it refreshes. Or " + claude + "."
		case r[appClaudeDesktop] > 0:
			return "The Claude desktop app keeps its sign-in to itself; " + claude + " to refresh Claude Code's."
		default:
			return title(claude) + " to open Claude Code and refresh its sign-in."
		}
	case "Codex":
		switch {
		case r[appCodex] > 0:
			return "Codex is running; its next message updates its log, and limitwatch reads it right away. Or " + codex + "."
		case r[appChatGPT] > 0:
			return "The ChatGPT app doesn't share its sign-in or limits; " + codex + " to sign in Codex."
		default:
			return title(codex) + " to sign in."
		}
	}
	return ""
}
