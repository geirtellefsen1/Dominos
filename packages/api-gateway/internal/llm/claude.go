// Package llm is a minimal Anthropic Messages API client used by the
// Phase 7 triage engine. When DOMINION_ANTHROPIC_API_KEY is unset we
// fall back to a deterministic stub so the MVP runs end-to-end without
// an API key.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	DefaultModel    = "claude-haiku-4-5-20251001"
	DefaultEndpoint = "https://api.anthropic.com/v1/messages"
)

type Client struct {
	apiKey   string
	model    string
	endpoint string
	http     *http.Client
}

func New() *Client {
	return &Client{
		apiKey:   os.Getenv("DOMINION_ANTHROPIC_API_KEY"),
		model:    envOr("DOMINION_ANTHROPIC_MODEL", DefaultModel),
		endpoint: envOr("DOMINION_ANTHROPIC_ENDPOINT", DefaultEndpoint),
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// IsStub reports whether the client has no live API key and is running
// in deterministic-fallback mode.
func (c *Client) IsStub() bool { return c.apiKey == "" }

// DraftReply returns either a draft body or a SKIP reason (both as
// strings, plus a bool signalling which). The prompt intentionally asks
// the model to emit "SKIP: <reason>" when the email is automated /
// marketing / noise — the caller uses that to decide whether to create
// a draft.v1 document.
func (c *Client) DraftReply(ctx context.Context, prompt string) (reply string, skipReason string, err error) {
	if c.IsStub() {
		// Deterministic fallback so the smoke test works without a key.
		reply = "Thanks — I'll follow up shortly.\n\n— drafted by Astrid"
		return reply, "", nil
	}
	body := map[string]any{
		"model":      c.model,
		"max_tokens": 512,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
	}
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("anthropic %s: %s", resp.Status, string(raw))
	}

	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", err
	}
	text := strings.TrimSpace(joinText(out.Content))
	if text == "" {
		return "", "", errors.New("empty model response")
	}
	if strings.HasPrefix(text, "SKIP:") {
		return "", strings.TrimSpace(strings.TrimPrefix(text, "SKIP:")), nil
	}
	return text, "", nil
}

func joinText(blocks []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	var b strings.Builder
	for _, c := range blocks {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func envOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
