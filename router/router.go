package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"ghost-protocol/classifier"
	"ghost-protocol/config"
)

// Destination tells the caller where the request was sent.
type Destination string

const (
	DestLocal  Destination = "local"
	DestCloud  Destination = "cloud"
	DestCache  Destination = "cache"
)

// Router dispatches requests to local Ollama or the cloud provider.
type Router struct {
	cfg        config.Config
	httpClient *http.Client
}

// New creates a Router.
func New(cfg config.Config) *Router {
	return &Router{
		cfg: cfg,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// ChatRequest is an OpenAI-compatible chat completion request body.
type ChatRequest struct {
	Model    string                   `json:"model"`
	Messages []map[string]interface{} `json:"messages"`
	Stream   bool                     `json:"stream"`
}

// Forward sends the request to the appropriate backend.
// It first tries the preferred backend; on failure it fails over to the other.
func (r *Router) Forward(ctx context.Context, body []byte, complexity classifier.Complexity) ([]byte, Destination, error) {
	if complexity == classifier.Trivial {
		resp, err := r.sendToLocal(ctx, body)
		if err == nil {
			return resp, DestLocal, nil
		}
		// Failover to cloud
		fmt.Printf("[router] local failed (%v), failing over to cloud\n", err)
		resp, err = r.sendToCloud(ctx, body)
		if err != nil {
			return nil, DestCloud, fmt.Errorf("both local and cloud failed: %w", err)
		}
		return resp, DestCloud, nil
	}

	// Complex — try cloud first, failover to local
	resp, err := r.sendToCloud(ctx, body)
	if err == nil {
		return resp, DestCloud, nil
	}
	fmt.Printf("[router] cloud failed (%v), failing over to local\n", err)
	resp, err = r.sendToLocal(ctx, body)
	if err != nil {
		return nil, DestLocal, fmt.Errorf("both cloud and local failed: %w", err)
	}
	return resp, DestLocal, nil
}

func (r *Router) sendToLocal(ctx context.Context, body []byte) ([]byte, error) {
	// Rewrite the model field to the configured local model.
	body, err := rewriteModel(body, r.cfg.Local.Model)
	if err != nil {
		return nil, err
	}
	url := r.cfg.Local.BaseURL + "/v1/chat/completions"
	return r.post(ctx, url, body, "")
}

func (r *Router) sendToCloud(ctx context.Context, body []byte) ([]byte, error) {
	// Rewrite the model field to the configured cloud model.
	body, err := rewriteModel(body, r.cfg.Cloud.Model)
	if err != nil {
		return nil, err
	}
	url := r.cfg.Cloud.BaseURL + "/v1/chat/completions"
	return r.post(ctx, url, body, r.cfg.Cloud.APIKey)
}

func (r *Router) post(ctx context.Context, url string, body []byte, apiKey string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post to %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream %s returned %d: %s", url, resp.StatusCode, string(b))
	}
	return io.ReadAll(resp.Body)
}

// rewriteModel swaps the "model" field in a JSON request body.
func rewriteModel(body []byte, model string) ([]byte, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("rewrite model: unmarshal: %w", err)
	}
	m["model"] = model
	return json.Marshal(m)
}
