package console

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/termrouter/termrouter/internal/execution"
	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/provider"
	"github.com/termrouter/termrouter/internal/provider/anthropic"
	"github.com/termrouter/termrouter/internal/provider/compatible"
	"github.com/termrouter/termrouter/internal/router"
	"github.com/termrouter/termrouter/internal/smart"
)

// handlePlayground runs a real completion through the resolver + coordinator
// using the admin console session (no client key required).
func (s *Server) handlePlayground(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream bool `json:"stream"`
		// Optional explicit route id when model is an internal route name.
		Route string `json:"route,omitempty"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	model := strings.TrimSpace(body.Model)
	if model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "model is required")
		return
	}
	if len(body.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "messages are required")
		return
	}

	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}

	msgs := make([]normalization.Message, 0, len(body.Messages))
	var systemText string
	for _, m := range body.Messages {
		role := normalization.Role(strings.ToLower(strings.TrimSpace(m.Role)))
		if role == "" {
			role = normalization.RoleUser
		}
		if role == normalization.RoleSystem {
			systemText = m.Content
		}
		msgs = append(msgs, normalization.Message{
			Role: role,
			Content: []normalization.ContentBlock{{
				Type: normalization.ContentText,
				Text: m.Content,
			}},
		})
	}

	nreq := &normalization.NormalizedRequest{
		ID:             "console_playground",
		RequestedModel: model,
		Messages:       msgs,
		System:         systemText,
		Stream:         false, // non-stream for admin playground MVP
	}

	resolver := router.NewResolver(rc.Cfg)
	plan, err := resolver.Resolve(model, rc.Cfg.Server.AllowDirectModel)
	if err != nil {
		for an := range rc.Cfg.Aliases {
			if strings.EqualFold(an, model) {
				plan, err = resolver.Resolve(an, false)
				break
			}
		}
		if plan == nil {
			writeError(w, http.StatusNotFound, "model_not_found", err.Error())
			return
		}
	}
	nreq.ResolvedAlias = plan.Alias
	if plan.PublicModel != "" {
		nreq.RequestedModel = plan.PublicModel
	}

	credCheck := func(ref string) bool {
		if s.Creds == nil {
			return true
		}
		_, e := s.Creds.Resolve(ref)
		return e == nil
	}
	eng := smart.GatewayEngine(rc.Cfg, s.Store, credCheck)

	var decision *smart.Decision
	if plan.Strategy == "smart" {
		applied, aerr := smart.ApplySmart(r.Context(), eng, rc.Cfg, s.Store, plan, nreq, r)
		if aerr != nil {
			writeError(w, http.StatusBadGateway, "smart_error", aerr.Error())
			return
		}
		plan = applied.Plan
		decision = applied.Decision
	}

	reg := provider.NewRegistry()
	reg.Register(compatible.NewOpenAI())
	reg.Register(compatible.NewCompatible())
	reg.Register(anthropic.New())
	coord := execution.New(reg, s.Creds, s.Store, s.Log)
	coord.Cfg = rc.Cfg

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	start := time.Now()
	result, err := coord.Execute(ctx, nreq, plan, execution.PolicyContext{ClientKey: nil, PublicHosting: rc.Cfg.PublicHosting.Enabled})
	lat := time.Since(start)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_error", sanitizeErr(err))
		return
	}

	// Extract text content from normalized response.
	var text strings.Builder
	if result.Response != nil {
		for _, b := range result.Response.Content {
			if b.Type == normalization.ContentText {
				text.WriteString(b.Text)
			}
		}
	}

	out := map[string]any{
		"ok":              true,
		"response":        text.String(),
		"latency_ms":      lat.Milliseconds(),
		"provider":        result.ProviderID,
		"upstream_model":  result.UpstreamModel,
		"attempt":         result.Attempt,
		"fallback_reason": result.FallbackReason,
		"usage": map[string]any{
			"input_tokens":  0,
			"output_tokens": 0,
		},
	}
	if result.Response != nil {
		out["usage"] = map[string]any{
			"input_tokens":  result.Response.Usage.InputTokens,
			"output_tokens": result.Response.Usage.OutputTokens,
		}
		out["stop_reason"] = result.Response.StopReason
	}
	if decision != nil {
		out["decision"] = decision
	} else {
		out["decision"] = map[string]any{
			"selected_provider": result.ProviderID,
			"selected_model":    result.UpstreamModel,
			"mode":              plan.Strategy,
		}
	}
	writeJSON(w, http.StatusOK, out)
}
