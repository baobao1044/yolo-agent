package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// UserProfile stores distilled facts about the user.
type UserProfile struct {
	UserID         string            `json:"user_id"`
	Name           string            `json:"name,omitempty"`
	Preferences    map[string]string `json:"preferences,omitempty"`
	Facts          []string          `json:"facts,omitempty"`
	LastUpdated    time.Time         `json:"last_updated"`
	mu             sync.RWMutex
}

// ProfileStore manages user profiles.
type ProfileStore struct {
	vs *VectorStore
}

// NewProfileStore creates a new profile store.
func NewProfileStore(vs *VectorStore) *ProfileStore {
	return &ProfileStore{vs: vs}
}

// Get loads or creates a profile for a user.
func (p *ProfileStore) Get(ctx context.Context, userID string) (*UserProfile, error) {
	mems, err := p.vs.GetByScope(ctx, profileScope(userID), 100)
	if err != nil {
		return nil, err
	}

	prof := &UserProfile{
		UserID:      userID,
		Preferences: make(map[string]string),
	}

	for _, m := range mems {
		switch m.Source {
		case "preference":
			parts := strings.SplitN(m.Content, "=", 2)
			if len(parts) == 2 {
				prof.Preferences[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		case "fact":
			prof.Facts = append(prof.Facts, m.Content)
		case "name":
			prof.Name = m.Content
		}
	}

	return prof, nil
}

// ExtractFacts takes a user message and returns extracted facts JSON.
// In a full implementation this calls an LLM; here we provide a mock extractor
// plus a real example prompt that can be sent to the LLM.
func (p *ProfileStore) ExtractFacts(ctx context.Context, message string) ([]Memory, error) {
	// Naive extraction: look for patterns like "I live in X", "My name is X", "I like X"
	var memories []Memory

	message = strings.ToLower(message)

	if idx := strings.Index(message, "my name is "); idx >= 0 {
		rest := message[idx+len("my name is "):]
		if end := strings.IndexAny(rest, ".;,"); end > 0 {
			rest = rest[:end]
		}
		memories = append(memories, Memory{
			Scope:      "user_default",
			Source:     "name",
			Content:    strings.TrimSpace(rest),
			Importance: 1.0,
		})
	}

	if idx := strings.Index(message, "i like "); idx >= 0 {
		rest := message[idx+len("i like "):]
		if end := strings.IndexAny(rest, ".;,"); end > 0 {
			rest = rest[:end]
		}
		memories = append(memories, Memory{
			Scope:      "user_default",
			Source:     "preference",
			Content:    "like=" + strings.TrimSpace(rest),
			Importance: 0.8,
		})
	}

	if idx := strings.Index(message, "i live in "); idx >= 0 {
		rest := message[idx+len("i live in "):]
		if end := strings.IndexAny(rest, ".;,"); end > 0 {
			rest = rest[:end]
		}
		memories = append(memories, Memory{
			Scope:      "user_default",
			Source:     "fact",
			Content:    "lives in " + strings.TrimSpace(rest),
			Importance: 0.9,
		})
	}

	return memories, nil
}

// SaveMemories stores extracted memories.
func (p *ProfileStore) SaveMemories(ctx context.Context, memories []Memory) error {
	for _, m := range memories {
		if m.Scope == "" {
			m.Scope = "user_default"
		}
		_, err := p.vs.Store(ctx, m)
		if err != nil {
			return err
		}
	}
	return nil
}

// LLMExtractionPrompt is a prompt that can be sent to an LLM to extract facts/preferences.
func (p *ProfileStore) LLMExtractionPrompt(message string) string {
	return fmt.Sprintf(`Extract any user facts, preferences, or identity information from the message below.
Return ONLY a JSON array of objects with fields: scope, source, content, importance.

Message: %s

Example output:
[
  {"scope":"user_default","source":"name","content":"Alice","importance":1.0},
  {"scope":"user_default","source":"preference","content":"theme=dark","importance":0.8}
]`, message)
}

// ParseExtractedFacts parses the LLM output into memories.
func ParseExtractedFacts(raw []byte) ([]Memory, error) {
	var mems []Memory
	if err := json.Unmarshal(raw, &mems); err != nil {
		return nil, err
	}
	return mems, nil
}

// profileScope returns the memory scope for a user profile.
func profileScope(userID string) string {
	return "profile_" + userID
}

// SummaryText returns a text summary of the profile for prompt injection.
func (p *UserProfile) SummaryText() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var parts []string
	if p.Name != "" {
		parts = append(parts, fmt.Sprintf("User name: %s", p.Name))
	}
	if len(p.Preferences) > 0 {
		parts = append(parts, "User preferences:")
		for k, v := range p.Preferences {
			parts = append(parts, fmt.Sprintf("  - %s: %s", k, v))
		}
	}
	if len(p.Facts) > 0 {
		parts = append(parts, "Known facts:")
		for _, f := range p.Facts {
			parts = append(parts, fmt.Sprintf("  - %s", f))
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}
