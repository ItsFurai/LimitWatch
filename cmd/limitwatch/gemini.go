package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Gemini CLI (Gemini Code Assist for individuals, Google AI Pro / Ultra)
// reports per-model daily quotas through the Code Assist endpoint the CLI
// itself uses, signed in with the CLI's saved Google sign-in. Like the other
// app sign-ins, limitwatch only reads it; running `gemini` refreshes it.
var geminiAPI = "https://cloudcode-pa.googleapis.com/v1internal"

func geminiDir() string {
	if d := os.Getenv("GEMINI_CLI_HOME"); d != "" {
		return filepath.Join(d, ".gemini")
	}
	return filepath.Join(homeDir(), ".gemini")
}

func fetchGemini(ctx context.Context, _ Provider) Report {
	r := Report{Provider: "Gemini", Fetched: time.Now()}
	raw, err := os.ReadFile(filepath.Join(geminiDir(), "oauth_creds.json"))
	if err != nil {
		return failed("Gemini", errMissing{"not signed in to Gemini CLI with a Google account"})
	}
	var creds struct {
		AccessToken string `json:"access_token"`
		ExpiryDate  int64  `json:"expiry_date"` // Unix milliseconds
	}
	if json.Unmarshal(raw, &creds) != nil || creds.AccessToken == "" {
		return failed("Gemini", errMissing{"no Google sign-in in Gemini CLI's settings"})
	}
	if creds.ExpiryDate > 0 && time.UnixMilli(creds.ExpiryDate).Before(time.Now()) {
		r.Err = "Gemini CLI's saved sign-in expired " + humanAge(time.UnixMilli(creds.ExpiryDate), time.Now())
		return r
	}
	auth := map[string]string{"Authorization": "Bearer " + creds.AccessToken}

	project := firstEnv("GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_PROJECT_ID")
	var load struct {
		Project     json.RawMessage `json:"cloudaicompanionProject"`
		CurrentTier struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"currentTier"`
	}
	meta := map[string]any{"metadata": map[string]string{"ideType": "IDE_UNSPECIFIED", "platform": "PLATFORM_UNSPECIFIED", "pluginType": "GEMINI"}}
	if err := getJSON(ctx, geminiAPI+":loadCodeAssist", auth, meta, &load); err != nil {
		r.Err = err.Error()
		return r
	}
	r.Plan = geminiPlan(load.CurrentTier.ID, load.CurrentTier.Name)
	if project == "" {
		project = geminiProject(load.Project)
	}
	var quota json.RawMessage
	if err := getJSON(ctx, geminiAPI+":retrieveUserQuota", auth, map[string]string{"project": project}, &quota); err != nil {
		r.Err = err.Error()
		return r
	}
	r.Limits, err = parseGeminiQuota(quota)
	if err != nil {
		r.Err = err.Error()
	}
	return r
}

// geminiProject reads cloudaicompanionProject, which is either a project ID
// string or an object with an id.
func geminiProject(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &o)
	return o.ID
}

func geminiPlan(id, name string) string {
	switch {
	case strings.Contains(id, "free"):
		return "Free"
	case strings.Contains(id, "standard"):
		return "Standard"
	case strings.Contains(id, "legacy"):
		return "Legacy"
	case name != "":
		return name
	}
	return title(id)
}

func parseGeminiQuota(body []byte) ([]Limit, error) {
	var q struct {
		Buckets []struct {
			ModelID           string   `json:"modelId"`
			TokenType         string   `json:"tokenType"`
			RemainingFraction *float64 `json:"remainingFraction"`
			ResetTime         string   `json:"resetTime"`
		} `json:"buckets"`
	}
	if err := json.Unmarshal(body, &q); err != nil {
		return nil, errors.New("unexpected reply from the Gemini quota API: " + err.Error())
	}
	seen := map[string]bool{}
	var out []Limit
	for _, b := range q.Buckets {
		model := strings.TrimSuffix(b.ModelID, "_vertex")
		if b.RemainingFraction == nil || model == "" || seen[model] || (b.TokenType != "" && b.TokenType != "REQUESTS") {
			continue
		}
		seen[model] = true
		out = append(out, Limit{
			ID: "gemini/" + model, Name: geminiModelName(model), Used: clampPct((1 - *b.RemainingFraction) * 100),
			Window: 24 * time.Hour, ResetsAt: parseTimeAny(b.ResetTime),
		})
	}
	if len(out) == 0 {
		return nil, errors.New("the Gemini quota API returned no limits")
	}
	// Pro models first, then the rest by name.
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := strings.Contains(out[i].ID, "pro"), strings.Contains(out[j].ID, "pro")
		if pi != pj {
			return pi
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

// geminiModelName shortens "gemini-2.5-flash-lite" to "2.5 Flash Lite".
func geminiModelName(model string) string {
	parts := strings.Split(strings.TrimPrefix(model, "gemini-"), "-")
	for i, p := range parts {
		parts[i] = title(p)
	}
	return strings.Join(parts, " ")
}
