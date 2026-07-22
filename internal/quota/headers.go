package quota

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// knownQuotaHeaders maps lowercase header names to extraction metadata.
// Only these headers are ever stored; authorization/cookies are never listed.
var knownQuotaHeaders = map[string]struct {
	dimension QuotaDimension
	kind      string // limit | remaining | reset | reset_requests | reset_tokens
}{
	// OpenAI-style
	"x-ratelimit-limit-requests":     {DimRequests, "limit"},
	"x-ratelimit-remaining-requests": {DimRequests, "remaining"},
	"x-ratelimit-reset-requests":     {DimRequests, "reset"},
	"x-ratelimit-limit-tokens":       {DimTotalTokens, "limit"},
	"x-ratelimit-remaining-tokens":   {DimTotalTokens, "remaining"},
	"x-ratelimit-reset-tokens":       {DimTotalTokens, "reset"},
	// Anthropic-style
	"anthropic-ratelimit-requests-limit":          {DimRequests, "limit"},
	"anthropic-ratelimit-requests-remaining":      {DimRequests, "remaining"},
	"anthropic-ratelimit-requests-reset":          {DimRequests, "reset"},
	"anthropic-ratelimit-tokens-limit":            {DimTotalTokens, "limit"},
	"anthropic-ratelimit-tokens-remaining":        {DimTotalTokens, "remaining"},
	"anthropic-ratelimit-tokens-reset":            {DimTotalTokens, "reset"},
	"anthropic-ratelimit-input-tokens-limit":      {DimInputTokens, "limit"},
	"anthropic-ratelimit-input-tokens-remaining":  {DimInputTokens, "remaining"},
	"anthropic-ratelimit-input-tokens-reset":      {DimInputTokens, "reset"},
	"anthropic-ratelimit-output-tokens-limit":     {DimOutputTokens, "limit"},
	"anthropic-ratelimit-output-tokens-remaining": {DimOutputTokens, "remaining"},
	"anthropic-ratelimit-output-tokens-reset":     {DimOutputTokens, "reset"},
}

// ExtractQuotaHeaders parses known provider rate-limit / quota headers into
// sanitized observations. Unknown headers and secrets are ignored.
func ExtractQuotaHeaders(providerID, accountID, modelID string, hdr http.Header, now time.Time) []QuotaHeaderObservation {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	// Aggregate by dimension so limit/remaining/reset merge into one observation.
	type acc struct {
		limit, remaining *float64
		resetAt          *time.Time
		name             string
	}
	byDim := map[QuotaDimension]*acc{}

	for name, values := range hdr {
		if len(values) == 0 {
			continue
		}
		key := strings.ToLower(name)
		// Explicitly refuse secrets.
		if isSecretHeader(key) {
			continue
		}
		meta, ok := knownQuotaHeaders[key]
		if !ok {
			continue
		}
		a := byDim[meta.dimension]
		if a == nil {
			a = &acc{name: name}
			byDim[meta.dimension] = a
		}
		raw := strings.TrimSpace(values[0])
		switch meta.kind {
		case "limit":
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				a.limit = &v
			}
		case "remaining":
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				a.remaining = &v
			}
		case "reset":
			if t, ok := parseResetValue(raw, now); ok {
				a.resetAt = &t
			}
		}
		a.name = name
	}

	out := make([]QuotaHeaderObservation, 0, len(byDim))
	for dim, a := range byDim {
		out = append(out, QuotaHeaderObservation{
			ProviderID:    providerID,
			AccountID:     accountID,
			ModelID:       modelID,
			Dimension:     dim,
			Limit:         a.limit,
			Remaining:     a.remaining,
			ResetAt:       a.resetAt,
			ObservedAt:    now.UTC(),
			RawHeaderName: a.name,
			Source:        SourceProviderHeader,
		})
	}
	return out
}

func isSecretHeader(name string) bool {
	switch name {
	case "authorization", "proxy-authorization", "cookie", "set-cookie",
		"x-api-key", "api-key", "x-auth-token":
		return true
	}
	if strings.Contains(name, "authorization") || strings.Contains(name, "cookie") {
		return true
	}
	return false
}

// parseResetValue accepts RFC3339 timestamps, unix seconds, or OpenAI-style
// duration strings like "1s", "6m0s", "1h30m0s".
func parseResetValue(raw string, now time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	// RFC3339
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), true
	}
	// Unix seconds or milliseconds
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		if f > 1e12 {
			// ms
			return time.UnixMilli(int64(f)).UTC(), true
		}
		if f > 1e9 {
			return time.Unix(int64(f), 0).UTC(), true
		}
		// small number: treat as seconds-from-now
		return now.UTC().Add(time.Duration(f * float64(time.Second))), true
	}
	// Duration string
	if d, err := time.ParseDuration(raw); err == nil {
		return now.UTC().Add(d), true
	}
	// OpenAI sometimes uses "6m0s" which ParseDuration handles; also "1s".
	return time.Time{}, false
}

// ObservationsToWindows converts header observations into provisional window states.
func ObservationsToWindows(obs []QuotaHeaderObservation, now time.Time) []QuotaWindowState {
	out := make([]QuotaWindowState, 0, len(obs))
	for _, o := range obs {
		var used float64
		var limit *float64
		var remaining *float64
		if o.Limit != nil {
			limit = o.Limit
			if o.Remaining != nil {
				used = *o.Limit - *o.Remaining
				if used < 0 {
					used = 0
				}
				remaining = o.Remaining
			}
		} else if o.Remaining != nil {
			remaining = o.Remaining
		}
		util := Utilization(limit, used)
		staleAfter := now.Add(5 * time.Minute)
		fc := ForecastExhaustion(ForecastInput{
			Limit: limit, Used: used, Reserved: 0, BurnRate: 0,
			Now: now, ResetAt: o.ResetAt, Confidence: 0.7,
		})
		status := DeriveStatus(util, fc.Status, now, staleAfter, limit != nil)
		out = append(out, QuotaWindowState{
			DefinitionID:      "header:" + string(o.Dimension),
			ProviderID:        o.ProviderID,
			AccountID:         o.AccountID,
			ModelID:           o.ModelID,
			Dimension:         o.Dimension,
			Unit:              unitForDim(o.Dimension),
			WindowType:        WindowProviderReported,
			WindowStart:       now,
			ResetAt:           o.ResetAt,
			Limit:             limit,
			Used:              used,
			Reserved:          0,
			Remaining:         remaining,
			Utilization:       util,
			ForecastExhaustAt: fc.ForecastExhaustAt,
			ForecastStatus:    fc.Status,
			Status:            status,
			Source:            SourceProviderHeader,
			Confidence:        0.7,
			LastUpdatedAt:     o.ObservedAt,
			StaleAfter:        staleAfter,
			ProviderUsed:      Float64(used),
			Label:             "Provider rate limit (" + string(o.Dimension) + ")",
		})
	}
	return out
}

func unitForDim(d QuotaDimension) QuotaUnit {
	switch d {
	case DimEstimatedCost, DimBilledCost:
		return UnitUSD
	case DimRequests, DimConcurrentRequests:
		return UnitCount
	case DimCredits:
		return UnitProviderCredit
	case DimProviderCompute:
		return UnitComputeUnit
	default:
		return UnitTokens
	}
}
