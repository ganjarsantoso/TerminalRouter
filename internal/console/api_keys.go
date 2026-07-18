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

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Store.ListClientKeys(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "keys_error", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{
			"id":              k.ID,
			"name":            k.Name,
			"prefix":          k.KeyPrefix + "…",
			"enabled":         k.Enabled,
			"allowed_aliases": k.AllowedAliases,
			"created_at":      k.CreatedAt.UTC().Format(time.RFC3339),
			"rotated_at":      formatTimePtr(k.RotatedAt),
			"disabled_at":     formatTimePtr(k.DisabledAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": out})
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string   `json:"name"`
		Aliases []string `json:"aliases"`
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
	}
	if err := s.Store.InsertClientKey(r.Context(), k); err != nil {
		writeError(w, http.StatusInternalServerError, "save_error", err.Error())
		return
	}
	// Plaintext is returned exactly once.
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     id,
		"name":   body.Name,
		"key":    pt,
		"prefix": prefix,
		"note":   "Save this key now; only its hash is retained.",
	})
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
	writeJSON(w, http.StatusOK, map[string]any{
		"id":   id,
		"key":  pt,
		"note": "Save this key now; only its hash is retained.",
	})
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
