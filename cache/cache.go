package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"

	"ghost-protocol/config"
	"ghost-protocol/db"
)

// Cache provides semantic similarity-based response caching.
type Cache struct {
	db        *db.DB
	threshold float64
}

// New creates a Cache backed by the given database.
func New(database *db.DB, cfg config.CacheConfig) *Cache {
	return &Cache{
		db:        database,
		threshold: cfg.SimilarityThreshold,
	}
}

// PromptHash returns a stable hex hash of the prompt text (used as a DB key).
func PromptHash(prompt string) string {
	h := sha256.Sum256([]byte(prompt))
	return fmt.Sprintf("%x", h)
}

// Lookup checks for a semantically similar cached response.
// embedding is the vector for the current prompt (from Ollama).
// Returns the cached response and true on a hit, or "", false on a miss.
func (c *Cache) Lookup(embedding []float64) (string, bool, error) {
	rows, err := c.db.AllCacheEntries()
	if err != nil {
		return "", false, fmt.Errorf("cache lookup: %w", err)
	}
	best := -1.0
	bestResponse := ""
	for _, row := range rows {
		var stored []float64
		if err := json.Unmarshal(row.EmbeddingJSON, &stored); err != nil {
			continue
		}
		sim := cosineSimilarity(embedding, stored)
		if sim > best {
			best = sim
			bestResponse = row.Response
		}
	}
	if best >= c.threshold {
		return bestResponse, true, nil
	}
	return "", false, nil
}

// Store saves a prompt embedding and its response to the database.
func (c *Cache) Store(prompt string, embedding []float64, response string) error {
	embJSON, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("marshal embedding: %w", err)
	}
	hash := PromptHash(prompt)
	return c.db.SaveCacheEntry(hash, embJSON, response)
}

// cosineSimilarity computes the cosine similarity between two equal-length vectors.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
