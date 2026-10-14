package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds the API keys and budgets set with `limitwatch key` and
// `limitwatch budget`. It lives in the user config folder, readable only by
// you. Environment variables always win over it.
type Config struct {
	Keys    map[string]string  `json:"keys,omitempty"`
	Budgets map[string]float64 `json:"budgets,omitempty"`
}

func configPath() string {
	if p := os.Getenv("LIMITWATCH_CONFIG"); p != "" {
		return p
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = homeDir()
	}
	return filepath.Join(d, "limitwatch", "config.json")
}

func loadConfig() Config {
	c := Config{}
	if b, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Keys == nil {
		c.Keys = map[string]string{}
	}
	if c.Budgets == nil {
		c.Budgets = map[string]float64{}
	}
	return c
}

func (c Config) save() error {
	if err := os.MkdirAll(filepath.Dir(configPath()), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := configPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, configPath())
}

// apiKey finds the key for a provider: its environment variables first,
// then the config file.
func apiKey(p Provider) (key, from string) {
	for _, env := range p.KeyEnv {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v, env
		}
	}
	if v := loadConfig().Keys[p.Key]; v != "" {
		return v, "config"
	}
	return "", ""
}

// needKey is the error for a provider that has no key yet.
func needKey(p Provider) error {
	return errMissing{"no API key yet (" + strings.Join(p.KeyEnv, " or ") + ")"}
}

func budgetFor(key string) float64 { return loadConfig().Budgets[key] }

func findProvider(key string) (Provider, error) {
	for _, p := range providers {
		if p.Key == strings.ToLower(key) {
			return p, nil
		}
	}
	return Provider{}, fmt.Errorf("unknown provider %q (choose from %s)", key, providerKeys(nil))
}

// providerKeys lists provider keys, optionally only those matching keep.
func providerKeys(keep func(Provider) bool) string {
	var ks []string
	for _, p := range providers {
		if keep == nil || keep(p) {
			ks = append(ks, p.Key)
		}
	}
	return strings.Join(ks, ", ")
}

func usesKey(p Provider) bool    { return len(p.KeyEnv) > 0 }
func usesBudget(p Provider) bool { return p.Budget }

// setKey stores a key for a provider that takes one.
func setKey(provider, key string) error {
	p, err := findProvider(provider)
	if err != nil {
		return err
	}
	if !usesKey(p) {
		return fmt.Errorf("%s uses its app's sign-in, not an API key", p.Name)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("the key is empty")
	}
	c := loadConfig()
	c.Keys[p.Key] = key
	return c.save()
}

func removeKey(provider string) error {
	p, err := findProvider(provider)
	if err != nil {
		return err
	}
	c := loadConfig()
	if _, ok := c.Keys[p.Key]; !ok {
		return fmt.Errorf("no key saved for %s", p.Key)
	}
	delete(c.Keys, p.Key)
	return c.save()
}

func setBudget(provider, amount string) error {
	p, err := findProvider(provider)
	if err != nil {
		return err
	}
	if !usesBudget(p) {
		return fmt.Errorf("budgets are for API spend (%s)", providerKeys(usesBudget))
	}
	v, err := strconv.ParseFloat(strings.TrimPrefix(strings.TrimSpace(amount), "$"), 64)
	if err != nil || v < 0 {
		return fmt.Errorf("%q isn't an amount like 50 or 120.5", amount)
	}
	c := loadConfig()
	if v == 0 {
		delete(c.Budgets, p.Key)
	} else {
		c.Budgets[p.Key] = v
	}
	return c.save()
}

// maskKey shows enough of a key to recognize it: "sk-a…f3c9".
func maskKey(k string) string {
	if len(k) <= 10 {
		return strings.Repeat("•", len(k))
	}
	return k[:4] + "…" + k[len(k)-4:]
}

// keyList describes where each key-based provider gets its key.
func keyList() string {
	var b strings.Builder
	c := loadConfig()
	for _, p := range providers {
		if !usesKey(p) {
			continue
		}
		key, from := apiKey(p)
		state := sFaint.Render("not set   " + strings.Join(p.KeyEnv, " or "))
		if key != "" {
			state = sOK.Render("✓ ") + sText.Render(maskKey(key)) + sDim.Render("  from "+from)
		}
		line := fmt.Sprintf("  %s %s", sKey.Render(fmt.Sprintf("%-14s", p.Key)), state)
		if budget := c.Budgets[p.Key]; budget > 0 {
			line += sDim.Render(fmt.Sprintf("  budget $%.2f/month", budget))
		}
		b.WriteString(line + "\n")
	}
	if _, err := os.Stat(configPath()); errors.Is(err, fs.ErrNotExist) {
		return b.String()
	}
	return b.String() + sFaint.Render("\n  saved in "+configPath()) + "\n"
}
