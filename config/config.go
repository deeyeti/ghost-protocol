package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config is the top-level structure mirroring config.json.
type Config struct {
	Proxy     ProxyConfig     `json:"proxy"`
	Cloud     CloudConfig     `json:"cloud"`
	Local     LocalConfig     `json:"local"`
	Cache     CacheConfig     `json:"cache"`
	Redaction RedactionConfig `json:"redaction"`
	Routing   RoutingConfig   `json:"routing"`
}

type ProxyConfig struct {
	Port int `json:"port"`
}

type CloudConfig struct {
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
}

type LocalConfig struct {
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	EmbedModel     string `json:"embed_model"`
	ClassifierModel string `json:"classifier_model"`
}

type CacheConfig struct {
	SimilarityThreshold float64 `json:"similarity_threshold"`
}

type RedactionPattern struct {
	Name  string `json:"name"`
	Regex string `json:"regex"`
}

type RedactionConfig struct {
	Patterns []RedactionPattern `json:"patterns"`
}

type RoutingConfig struct {
	MaxContextTokens   int `json:"max_context_tokens"`
	TrivialMaxPromptLen int `json:"trivial_max_prompt_len"`
}

// Load reads and parses config.json from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read config file %q: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("cannot parse config file: %w", err)
	}
	return &cfg, nil
}
