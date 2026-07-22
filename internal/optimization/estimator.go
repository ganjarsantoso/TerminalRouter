package optimization

import (
	"strings"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/normalization"
)

// fallbackEstimator provides a conservative character-based token estimate
// when no exact provider/model tokenizer is available. Its counts are always
// labeled "estimated" and scaled by the configured safety multiplier.
type fallbackEstimator struct {
	cfg config.TokenEstimationConfig
}

func newFallbackEstimator(cfg config.TokenEstimationConfig) *fallbackEstimator {
	c := cfg
	if c.FallbackCharsPerToken <= 0 {
		c.FallbackCharsPerToken = 3.5
	}
	if c.SafetyMultiplier <= 0 {
		c.SafetyMultiplier = 1.15
	}
	return &fallbackEstimator{cfg: c}
}

func (e *fallbackEstimator) Name() string { return "fallback-chars" }

func (e *fallbackEstimator) Supports(provider, model string) bool {
	return true // fallback always available
}

func (e *fallbackEstimator) countText(s string) int {
	if s == "" {
		return 0
	}
	base := float64(len(s)) / e.cfg.FallbackCharsPerToken
	return int(base * e.cfg.SafetyMultiplier)
}

func (e *fallbackEstimator) CountText(text string) (int, error) {
	return e.countText(text), nil
}

func (e *fallbackEstimator) CountRequest(req *normalization.NormalizedRequest) (TokenBreakdown, error) {
	var b TokenBreakdown
	b.System = e.countText(req.System)

	// Find the last user message index — it is the "current user turn".
	// Everything before it that is user/assistant belongs to historical messages.
	lastUserIdx := -1
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == normalization.RoleUser {
			lastUserIdx = i
			break
		}
	}

	for i, m := range req.Messages {
		text := messageText(m)
		switch m.Role {
		case normalization.RoleUser:
			if i == lastUserIdx {
				// Current user turn: counted only in CurrentUser.
				b.CurrentUser += e.countText(text)
			} else {
				// Historical user turn: counted only in MessageHistory.
				b.MessageHistory += e.countText(text)
			}
		case normalization.RoleTool:
			// Tool results: counted only in ToolResults.
			b.ToolResults += e.countText(text)
		default:
			// Assistant and other roles: counted only in MessageHistory.
			b.MessageHistory += e.countText(text)
		}
	}
	for _, t := range req.Tools {
		b.ToolDefinitions += e.countText(t.Name) + e.countText(t.Description) + e.countText(marshalMap(t.InputSchema))
	}
	// JSON overhead for serialization.
	b.ProtocolOverhead = e.countText(marshalRequest(req)) - (b.System + b.CurrentUser + b.MessageHistory + b.ToolDefinitions + b.ToolResults)
	if b.ProtocolOverhead < 0 {
		b.ProtocolOverhead = 0
	}
	b.Total = b.System + b.MessageHistory + b.CurrentUser + b.ToolDefinitions + b.ToolResults + b.ProtocolOverhead
	b.Source = "estimated"
	return b, nil
}

// Registry is a collection of token estimators tried in priority order.
type estimatorRegistry struct {
	estimators []TokenEstimator
	fallback   *fallbackEstimator
}

func newEstimatorRegistry(cfg config.TokenEstimationConfig, exact []TokenEstimator) *estimatorRegistry {
	r := &estimatorRegistry{fallback: newFallbackEstimator(cfg)}
	r.estimators = append(r.estimators, exact...)
	return r
}

// Estimate selects the most specific supported estimator (exact > family >
// compatible > fallback) for the provider/model and returns a breakdown.
func (r *estimatorRegistry) Estimate(req *normalization.NormalizedRequest, provider, model string) TokenBreakdown {
	for _, e := range r.estimators {
		if e.Supports(provider, model) {
			if b, err := e.CountRequest(req); err == nil {
				return b
			}
		}
	}
	b, _ := r.fallback.CountRequest(req)
	return b
}

// CountText estimates text tokens using the best available estimator.
func (r *estimatorRegistry) CountText(text, provider, model string) int {
	for _, e := range r.estimators {
		if e.Supports(provider, model) {
			if n, err := e.CountText(text); err == nil {
				return n
			}
		}
	}
	n, _ := r.fallback.CountText(text)
	return n
}

func messageText(m normalization.Message) string {
	var b strings.Builder
	for _, c := range m.Content {
		if c.Type == normalization.ContentText {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func marshalMap(v map[string]any) string {
	if v == nil {
		return ""
	}
	b, err := jsonMarshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func marshalRequest(req *normalization.NormalizedRequest) string {
	b, err := jsonMarshal(req)
	if err != nil {
		return ""
	}
	return string(b)
}
