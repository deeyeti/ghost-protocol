package redaction

import (
	"fmt"
	"regexp"
	"sync"

	"ghost-protocol/config"
)

// Redactor scrubs sensitive data from text and can restore it after a response.
type Redactor struct {
	patterns []*compiledPattern
}

type compiledPattern struct {
	name string
	re   *regexp.Regexp
}

// New compiles all regex patterns from the config.
func New(cfg config.RedactionConfig) (*Redactor, error) {
	r := &Redactor{}
	for _, p := range cfg.Patterns {
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			return nil, fmt.Errorf("invalid redaction regex %q: %w", p.Regex, err)
		}
		r.patterns = append(r.patterns, &compiledPattern{name: p.Name, re: re})
	}
	return r, nil
}

// Session holds per-request token state so we can reverse substitutions.
type Session struct {
	mu      sync.Mutex
	counter int
	forward map[string]string // token → original
	reverse map[string]string // original → token
}

// NewSession creates a fresh redaction session for a single request.
func NewSession() *Session {
	return &Session{
		forward: make(map[string]string),
		reverse: make(map[string]string),
	}
}

// Redact replaces all matching sensitive values in text with opaque tokens.
// Identical original values always get the same token within the session.
func (r *Redactor) Redact(s *Session, text string) string {
	for _, p := range r.patterns {
		text = p.re.ReplaceAllStringFunc(text, func(match string) string {
			s.mu.Lock()
			defer s.mu.Unlock()
			if tok, ok := s.reverse[match]; ok {
				return tok
			}
			s.counter++
			tok := fmt.Sprintf("[REDACTED_%s_%d]", p.name, s.counter)
			s.forward[tok] = match
			s.reverse[match] = tok
			return tok
		})
	}
	return text
}

// Restore replaces all tokens in text with their original values.
func (r *Redactor) Restore(s *Session, text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for tok, original := range s.forward {
		// Simple string replace — tokens are unique and non-overlapping.
		re := regexp.MustCompile(regexp.QuoteMeta(tok))
		text = re.ReplaceAllString(text, original)
	}
	return text
}
