package middleware

import (
	"fmt"
	"strings"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/storage"
)

// ApplyRequestPolicy enforces server-level and per-key request constraints
// (message count, tool count, max output tokens) before provider execution.
func ApplyRequestPolicy(nreq *normalization.NormalizedRequest, cfg *config.Config, ck *storage.ClientKey) *normalization.Error {
	if nreq == nil {
		return nil
	}
	maxMessages := 200
	maxTools := 64
	if cfg != nil {
		if cfg.Server.MaxMessages > 0 {
			maxMessages = cfg.Server.MaxMessages
		}
		if cfg.Server.MaxTools > 0 {
			maxTools = cfg.Server.MaxTools
		}
	}
	if len(nreq.Messages) > maxMessages {
		return normalization.NewError(normalization.ErrInvalidRequest,
			fmt.Sprintf("request exceeds maximum message count (%d)", maxMessages), 400)
	}
	if len(nreq.Tools) > maxTools {
		return normalization.NewError(normalization.ErrInvalidRequest,
			fmt.Sprintf("request exceeds maximum tool count (%d)", maxTools), 400)
	}

	// Cap max output tokens to the key policy when set.
	if ck != nil && ck.MaxOutputTokens != nil && *ck.MaxOutputTokens > 0 {
		capN := *ck.MaxOutputTokens
		if nreq.MaxOutputTokens == nil {
			nreq.MaxOutputTokens = &capN
		} else if *nreq.MaxOutputTokens > capN {
			nreq.MaxOutputTokens = &capN
		}
	}

	// Optional non-secret client label from request metadata (never authorization).
	if nreq.Metadata != nil {
		if v, ok := nreq.Metadata["termrouter_client_name"].(string); ok {
			v = strings.TrimSpace(v)
			if v != "" {
				if len(v) > 64 {
					v = v[:64]
				}
				nreq.Metadata["termrouter_client_name"] = v
			}
		}
	}
	return nil
}

// AuthorizeModel enforces per-key model authorization. Punctuation in the model
// string is never treated as authorization. A requested model is authorized in
// exactly one of two ways:
//
//	Public alias:   the requested string matches a configured alias -> the key's
//	                allowed_aliases policy applies.
//	Direct model:   provider/model (or provider:model) syntax -> this requires
//	                the server to permit direct models AND the key to permit
//	                direct models AND (when the key restricts direct models) an
//	                exact match against the key's allowed_direct_models list.
//
// isAlias must be true only when the requested string exactly matches a
// configured public alias.
func AuthorizeModel(ck *storage.ClientKey, model string, isAlias, serverAllowDirect bool) *normalization.Error {
	if isAlias {
		if ck != nil && !ck.AliasAllowed(model) {
			return normalization.NewError(normalization.ErrPermissionDenied,
				fmt.Sprintf("client key is not allowed to use model %q", model), 403)
		}
		return nil
	}

	// Not an alias: only direct-model syntax may be authorized here.
	isDirect := strings.Contains(model, "/") || strings.Contains(model, ":")
	if !isDirect {
		// Unknown, non-alias, non-direct model: deny.
		return normalization.NewError(normalization.ErrPermissionDenied,
			fmt.Sprintf("client key is not allowed to use model %q", model), 403)
	}
	if !serverAllowDirect {
		return normalization.NewError(normalization.ErrPermissionDenied,
			"direct-model access is disabled on this server", 403)
	}
	if ck != nil {
		if !ck.AllowDirectModels {
			return normalization.NewError(normalization.ErrPermissionDenied,
				fmt.Sprintf("client key is not allowed to use direct model %q", model), 403)
		}
		if !ck.DirectModelAllowed(model) {
			return normalization.NewError(normalization.ErrPermissionDenied,
				fmt.Sprintf("client key is not allowed to use direct model %q", model), 403)
		}
	}
	return nil
}

// CheckAliasAllowed is retained for backward compatibility. Prefer AuthorizeModel.
func CheckAliasAllowed(ck *storage.ClientKey, model string) *normalization.Error {
	if ck == nil {
		return nil
	}
	if !ck.AliasAllowed(model) && !strings.Contains(model, "/") && !strings.Contains(model, ":") {
		return normalization.NewError(normalization.ErrPermissionDenied,
			fmt.Sprintf("client key is not allowed to use model %q", model), 403)
	}
	return nil
}
