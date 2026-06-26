package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ghost-protocol/cache"
	"ghost-protocol/classifier"
	"ghost-protocol/config"
	"ghost-protocol/db"
	"ghost-protocol/redaction"
	"ghost-protocol/router"
)

// Server is the Ghost Protocol reverse-proxy HTTP server.
type Server struct {
	cfg        config.Config
	db         *db.DB
	cache      *cache.Cache
	redactor   *redaction.Redactor
	classifier *classifier.Classifier
	router     *router.Router
	httpClient *http.Client
}

// New constructs a fully wired Server.
func New(cfg config.Config, database *db.DB) (*Server, error) {
	red, err := redaction.New(cfg.Redaction)
	if err != nil {
		return nil, fmt.Errorf("build redactor: %w", err)
	}
	return &Server{
		cfg:        cfg,
		db:         database,
		cache:      cache.New(database, cfg.Cache),
		redactor:   red,
		classifier: classifier.New(cfg.Local, cfg.Routing),
		router:     router.New(cfg),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// Start binds the HTTP server and blocks.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.handleChat)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})

	addr := fmt.Sprintf(":%d", s.cfg.Proxy.Port)
	fmt.Printf("\n  👻 Ghost Protocol listening on http://localhost%s\n\n", addr)
	return http.ListenAndServe(addr, mux)
}

// handleChat is the core pipeline: redact → cache → classify → route → restore → respond.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// --- Parse request ---
	var reqMap map[string]interface{}
	if err := json.Unmarshal(rawBody, &reqMap); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// --- Extract prompt text for heuristics ---
	promptText := extractPromptText(reqMap)

	// --- 1. Redaction ---
	session := redaction.NewSession()
	redactedPrompt := s.redactor.Redact(session, promptText)

	// Rebuild the request body with redacted content
	redactedBody, err := rewritePrompt(rawBody, redactedPrompt)
	if err != nil {
		http.Error(w, "redaction failed", http.StatusInternalServerError)
		return
	}

	// --- 2. Semantic Cache Lookup ---
	embedding, err := s.getEmbedding(r.Context(), redactedPrompt)
	var dest router.Destination
	var responseBody string

	if err == nil {
		cached, hit, cacheErr := s.cache.Lookup(embedding)
		if cacheErr == nil && hit {
			dest = router.DestCache
			responseBody = cached
			latency := int(time.Since(start).Milliseconds())
			fmt.Printf("  [cache HIT]  ⚡ %dms  %.40s…\n", latency, redactedPrompt)
			_ = s.db.LogRequest(cache.PromptHash(redactedPrompt), string(dest), latency, 0, 0)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Ghost-Protocol-Source", "cache")
			w.Write([]byte(responseBody))
			return
		}
	}

	// --- 3. Complexity Classification ---
	complexity := s.classifier.Classify(r.Context(), redactedPrompt)

	// --- 4. Route to Local or Cloud ---
	respBytes, destination, err := s.router.Forward(r.Context(), redactedBody, complexity)
	if err != nil {
		http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
		return
	}
	dest = destination

	// --- 5. Restore redacted tokens in response ---
	responseBody = s.redactor.Restore(session, string(respBytes))

	// --- 6. Cache the result for future use ---
	if embedding != nil {
		_ = s.cache.Store(redactedPrompt, embedding, responseBody)
	}

	latency := int(time.Since(start).Milliseconds())
	icon := "☁️"
	if dest == router.DestLocal {
		icon = "🏠"
	}
	fmt.Printf("  [%-5s]  %s %dms  %.40s…\n", dest, icon, latency, redactedPrompt)
	_ = s.db.LogRequest(cache.PromptHash(redactedPrompt), string(dest), latency, 0, 0)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Ghost-Protocol-Source", string(dest))
	w.Header().Set("X-Ghost-Protocol-Complexity", complexity.String())
	w.Write([]byte(responseBody))
}

// getEmbedding calls Ollama's embedding endpoint for a text string.
func (s *Server) getEmbedding(ctx context.Context, text string) ([]float64, error) {
	reqBody, _ := json.Marshal(map[string]string{
		"model":  s.cfg.Local.EmbedModel,
		"prompt": text,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.cfg.Local.BaseURL+"/api/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Embedding, nil
}

// extractPromptText pulls the last user message content from an OpenAI-format request.
func extractPromptText(reqMap map[string]interface{}) string {
	msgs, ok := reqMap["messages"].([]interface{})
	if !ok || len(msgs) == 0 {
		return ""
	}
	var parts []string
	for _, m := range msgs {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		if content, ok := msg["content"].(string); ok {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n")
}

// rewritePrompt reconstructs the JSON body replacing all message content with redacted text.
func rewritePrompt(rawBody []byte, redactedText string) ([]byte, error) {
	var reqMap map[string]interface{}
	if err := json.Unmarshal(rawBody, &reqMap); err != nil {
		return nil, err
	}
	msgs, ok := reqMap["messages"].([]interface{})
	if !ok {
		return rawBody, nil
	}
	// Simple strategy: put redacted full text as the last user message content.
	// A more sophisticated approach would redact message-by-message.
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, ok := msgs[i].(map[string]interface{})
		if !ok {
			continue
		}
		if msg["role"] == "user" {
			msg["content"] = redactedText
			break
		}
	}
	reqMap["messages"] = msgs
	return json.Marshal(reqMap)
}
