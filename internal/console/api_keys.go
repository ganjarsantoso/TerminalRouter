package console

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/termrouter/termrouter/internal/credentials"
	"github.com/termrouter/termrouter/internal/storage"
)

const portableKeyWarning = "Portable keys are shared across devices. Compromise, rotation, and revocation affect every agent using the key. Treat the secret like a password with financial impact."

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Store.ListClientKeys(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "keys_error", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, keyToAPI(k))
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": out})
}

func keyToAPI(k storage.ClientKey) map[string]any {
	m := map[string]any{
		"id":              k.ID,
		"name":            k.Name,
		"prefix":          k.KeyPrefix + "…",
		"enabled":         k.Enabled,
		"allowed_aliases": k.AllowedAliases,
		"portable":        k.Portable,
		"created_at":      k.CreatedAt.UTC().Format(time.RFC3339),
		"rotated_at":      formatTimePtr(k.RotatedAt),
		"disabled_at":     formatTimePtr(k.DisabledAt),
		"expires_at":      formatTimePtr(k.ExpiresAt),
	}
	if k.RateLimitRPM != nil {
		m["rate_limit_rpm"] = *k.RateLimitRPM
	}
	if k.MaxConcurrentRequests != nil {
		m["max_concurrent_requests"] = *k.MaxConcurrentRequests
	}
	if k.DailyRequestLimit != nil {
		m["daily_request_limit"] = *k.DailyRequestLimit
	}
	if k.DailyInputTokens != nil {
		m["daily_input_tokens"] = *k.DailyInputTokens
	}
	if k.DailyOutputTokens != nil {
		m["daily_output_tokens"] = *k.DailyOutputTokens
	}
	if k.DailyEstimatedCostUSD != nil {
		m["daily_estimated_cost_usd"] = *k.DailyEstimatedCostUSD
	}
	if k.MaxOutputTokens != nil {
		m["max_output_tokens"] = *k.MaxOutputTokens
	}
	if k.MaxRequestBody != nil {
		m["max_request_body"] = *k.MaxRequestBody
	}
	if k.Portable {
		m["warning"] = portableKeyWarning
	}
	return m
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name                  string   `json:"name"`
		Aliases               []string `json:"aliases"`
		RateLimitRPM          *int     `json:"rate_limit_rpm"`
		MaxConcurrentRequests *int     `json:"max_concurrent_requests"`
		DailyRequestLimit     *int     `json:"daily_request_limit"`
		DailyInputTokens      *int64   `json:"daily_input_tokens"`
		DailyOutputTokens     *int64   `json:"daily_output_tokens"`
		DailyEstimatedCostUSD *float64 `json:"daily_estimated_cost_usd"`
		MaxOutputTokens       *int     `json:"max_output_tokens"`
		MaxRequestBody        *int64   `json:"max_request_body"`
		Portable              bool     `json:"portable"`
		ExpiresAt             string   `json:"expires_at"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		body.Name = "default"
	}
	pt, prefix, hash, salt, err := credentials.GenerateClientKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "keygen_error", err.Error())
		return
	}
	id := "key_" + randomHex(8)
	k := storage.ClientKey{
		ID: id, Name: body.Name, KeyPrefix: prefix, KeyHash: hash, Salt: salt,
		Enabled: true, AllowedAliases: body.Aliases, CreatedAt: time.Now().UTC(),
		RateLimitRPM: body.RateLimitRPM, MaxConcurrentRequests: body.MaxConcurrentRequests,
		DailyRequestLimit: body.DailyRequestLimit, DailyInputTokens: body.DailyInputTokens,
		DailyOutputTokens: body.DailyOutputTokens, DailyEstimatedCostUSD: body.DailyEstimatedCostUSD,
		MaxOutputTokens: body.MaxOutputTokens, MaxRequestBody: body.MaxRequestBody, Portable: body.Portable,
	}
	if body.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, body.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "expires_at must be RFC3339")
			return
		}
		k.ExpiresAt = &t
	}
	if err := s.Store.InsertClientKey(r.Context(), k); err != nil {
		writeError(w, http.StatusInternalServerError, "save_error", err.Error())
		return
	}
	// Plaintext is returned exactly once.
	resp := map[string]any{
		"id":     id,
		"name":   body.Name,
		"key":    pt,
		"prefix": prefix,
		"note":   "Save this key now; only its hash is retained.",
	}
	if body.Portable {
		resp["warning"] = portableKeyWarning
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleRotateKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.Store.GetClientKeyByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "keys_error", err.Error())
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "not_found", "client key not found")
		return
	}
	pt, prefix, hash, salt, err := credentials.GenerateClientKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "keygen_error", err.Error())
		return
	}
	if err := s.Store.UpdateClientKeyHash(r.Context(), id, prefix, hash, salt); err != nil {
		writeError(w, http.StatusInternalServerError, "save_error", err.Error())
		return
	}
	resp := map[string]any{
		"id":   id,
		"key":  pt,
		"note": "Save this key now; only its hash is retained.",
	}
	if existing.Portable {
		resp["warning"] = portableKeyWarning
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDisableKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Store.DisableClientKey(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"disabled": id})
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Store.RemoveClientKey(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": id})
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func formatTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}
