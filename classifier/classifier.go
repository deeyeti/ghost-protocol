package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ghost-protocol/config"
)

// Complexity is the result of prompt classification.
type Complexity int

const (
	Trivial Complexity = iota
	Complex
)

func (c Complexity) String() string {
	if c == Trivial {
		return "trivial"
	}
	return "complex"
}

// Classifier uses heuristics first, then a tiny local LLM as a tiebreaker.
type Classifier struct {
	cfg        config.LocalConfig
	routing    config.RoutingConfig
	httpClient *http.Client
}

// New creates a Classifier.
func New(local config.LocalConfig, routing config.RoutingConfig) *Classifier {
	return &Classifier{
		cfg:     local,
		routing: routing,
		httpClient: &http.Client{Timeout: 8 * time.Second},
	}
}

// Classify returns Trivial or Complex for the given prompt.
// It first applies fast heuristics; if inconclusive it asks the local LLM.
func (c *Classifier) Classify(ctx context.Context, prompt string) Complexity {
	// --- Fast heuristics ---
	if len(prompt) <= c.routing.TrivialMaxPromptLen {
		trivialKeywords := []string{
			"syntax", "fix", "typo", "rename", "format", "lint",
			"import", "semicolon", "bracket", "indent", "boilerplate",
			"hello world", "stub", "comment",
		}
		lower := strings.ToLower(prompt)
		for _, kw := range trivialKeywords {
			if strings.Contains(lower, kw) {
				return Trivial
			}
		}
	}

	complexKeywords := []string{
		"architect", "design", "refactor", "optimize", "migrate",
		"performance", "security", "database schema", "system design",
		"trade-off", "algorithm", "implement from scratch",
	}
	lower := strings.ToLower(prompt)
	for _, kw := range complexKeywords {
		if strings.Contains(lower, kw) {
			return Complex
		}
	}

	// --- LLM tiebreaker (non-blocking best-effort) ---
	result, err := c.askLLM(ctx, prompt)
	if err != nil {
		// If the classifier LLM is unavailable, default to cloud to be safe.
		return Complex
	}
	return result
}

type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []map[string]string `json:"messages"`
	Stream   bool                `json:"stream"`
}

type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func (c *Classifier) askLLM(ctx context.Context, prompt string) (Complexity, error) {
	systemMsg := `You are a task complexity classifier. 
Given a developer prompt, reply with ONLY one word: "trivial" or "complex".
trivial = simple syntax fixes, formatting, boilerplate generation, basic lookups.
complex = architecture, deep refactoring, security analysis, algorithm design.`

	reqBody := ollamaChatRequest{
		Model: c.cfg.ClassifierModel,
		Messages: []map[string]string{
			{"role": "system", "content": systemMsg},
			{"role": "user", "content": prompt},
		},
		Stream: false,
	}
	data, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.BaseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return Complex, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Complex, fmt.Errorf("classifier LLM unreachable: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var ollamaResp ollamaChatResponse
	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		return Complex, err
	}

	answer := strings.TrimSpace(strings.ToLower(ollamaResp.Message.Content))
	if strings.HasPrefix(answer, "trivial") {
		return Trivial, nil
	}
	return Complex, nil
}
