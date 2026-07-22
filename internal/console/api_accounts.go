package console

import (
	"net/http"
	"time"

	"github.com/termrouter/termrouter/internal/quota"
	"github.com/termrouter/termrouter/internal/storage"
)

// GET /admin/v1/providers/{provider}/accounts
func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	if providerID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "provider path parameter required")
		return
	}

	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}

	// Check provider exists.
	if _, ok := rc.Cfg.Providers[providerID]; !ok {
		writeError(w, http.StatusNotFound, "not_found", "provider "+providerID+" not found")
		return
	}

	// Build account list from stored operational state.
	ctx := r.Context()
	now := time.Now().UTC()
	states, err := s.Store.ListAccountOpStates(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_error", err.Error())
		return
	}

	// Load quota snapshots for these accounts.
	snaps, _ := s.Store.LatestQuotaSnapshotsForAccount(ctx, providerID, "")

	accounts := []map[string]any{}
	seen := map[string]bool{}
	for _, st := range states {
		if st.ProviderID != providerID {
			continue
		}
		seen[st.AccountID] = true
		acct := map[string]any{
			"id":                 st.AccountID,
			"provider_id":        st.ProviderID,
			"display_name":       st.AccountID,
			"enabled":            st.Enabled,
			"draining":           st.Draining,
			"credential_backend": "vault",
			"routing_weight":     1,
			"updated_at":         st.UpdatedAt.Format(time.RFC3339),
		}
		// Attach quota windows for this account.
		var wins []quota.QuotaWindowState
		for _, snap := range snaps {
			if snap.AccountID == st.AccountID {
				w := snapshotsToWindows([]storage.QuotaSnapshotRecord{snap}, now)
				if len(w) > 0 {
					wins = append(wins, w[0])
				}
			}
		}
		if len(wins) > 0 {
			acct["quota_windows"] = wins
		}
		accounts = append(accounts, acct)
	}

	// Add implicit default account if provider exists but has no explicit accounts.
	if !seen[quota.DefaultAccountID] {
		accounts = append(accounts, map[string]any{
			"id":                 quota.DefaultAccountID,
			"provider_id":        providerID,
			"display_name":       "Default",
			"enabled":            true,
			"draining":           false,
			"credential_backend": "vault",
			"routing_weight":     1,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"provider_id": providerID,
		"accounts":    accounts,
		"count":       len(accounts),
	})
}

// POST /admin/v1/providers/{provider}/accounts
func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	if providerID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "provider path parameter required")
		return
	}

	var req struct {
		ID             string `json:"id"`
		DisplayName    string `json:"display_name"`
		RoutingWeight  int    `json:"routing_weight"`
		QuotaRoutingOK bool   `json:"quota_routing_allowed"`
		RotationOK     bool   `json:"multi_account_rotation_allowed"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "id is required")
		return
	}

	ctx := r.Context()
	now := time.Now().UTC()

	err := s.Store.UpsertAccountOpState(ctx, storage.AccountOpState{
		ProviderID: providerID,
		AccountID:  req.ID,
		Enabled:    true,
		UpdatedAt:  now,
		Metadata: map[string]any{
			"display_name":                   req.DisplayName,
			"routing_weight":                 req.RoutingWeight,
			"quota_routing_allowed":          req.QuotaRoutingOK,
			"multi_account_rotation_allowed": req.RotationOK,
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create_error", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          req.ID,
		"provider_id": providerID,
		"status":      "created",
	})
}

// GET /admin/v1/providers/{provider}/accounts/{account}
func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	accountID := r.PathValue("account")
	if providerID == "" || accountID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "provider and account path parameters required")
		return
	}

	ctx := r.Context()
	now := time.Now().UTC()
	st, err := s.Store.GetAccountOpState(ctx, providerID, accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_error", err.Error())
		return
	}
	if st == nil {
		writeError(w, http.StatusNotFound, "not_found", "account not found")
		return
	}

	snaps, _ := s.Store.LatestQuotaSnapshotsForAccount(ctx, providerID, accountID)
	windows := snapshotsToWindows(snaps, now)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":            st.AccountID,
		"provider_id":   st.ProviderID,
		"enabled":       st.Enabled,
		"draining":      st.Draining,
		"updated_at":    st.UpdatedAt.Format(time.RFC3339),
		"metadata":      st.Metadata,
		"quota_windows": windows,
	})
}

// PATCH /admin/v1/providers/{provider}/accounts/{account}
func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	accountID := r.PathValue("account")
	if providerID == "" || accountID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "provider and account path parameters required")
		return
	}

	var req struct {
		Enabled        *bool  `json:"enabled"`
		Draining       *bool  `json:"draining"`
		DisplayName    string `json:"display_name"`
		RoutingWeight  *int   `json:"routing_weight"`
		QuotaRoutingOK *bool  `json:"quota_routing_allowed"`
		RotationOK     *bool  `json:"multi_account_rotation_allowed"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	ctx := r.Context()
	now := time.Now().UTC()

	existing, err := s.Store.GetAccountOpState(ctx, providerID, accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_error", err.Error())
		return
	}

	draining := false
	enabled := true
	meta := map[string]any{}
	if existing != nil {
		draining = existing.Draining
		enabled = existing.Enabled
		meta = existing.Metadata
	}
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.Draining != nil {
		draining = *req.Draining
	}
	if req.DisplayName != "" {
		meta["display_name"] = req.DisplayName
	}
	if req.RoutingWeight != nil {
		meta["routing_weight"] = *req.RoutingWeight
	}
	if req.QuotaRoutingOK != nil {
		meta["quota_routing_allowed"] = *req.QuotaRoutingOK
	}
	if req.RotationOK != nil {
		meta["multi_account_rotation_allowed"] = *req.RotationOK
	}

	err = s.Store.UpsertAccountOpState(ctx, storage.AccountOpState{
		ProviderID: providerID,
		AccountID:  accountID,
		Enabled:    enabled,
		Draining:   draining,
		UpdatedAt:  now,
		Metadata:   meta,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          accountID,
		"provider_id": providerID,
		"enabled":     enabled,
		"draining":    draining,
		"status":      "updated",
	})
}

// POST /admin/v1/providers/{provider}/accounts/{account}/test
func (s *Server) handleTestAccount(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	accountID := r.PathValue("account")
	if providerID == "" || accountID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "provider and account path parameters required")
		return
	}

	// Verify credential availability via resolve.
	credRef := "vault://" + providerID
	if accountID != quota.DefaultAccountID {
		credRef = "vault://" + providerID + "-" + accountID
	}
	_, err := s.Creds.Resolve(credRef)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"provider_id": providerID,
			"account_id":  accountID,
			"status":      "credential_unavailable",
			"error":       err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"provider_id": providerID,
		"account_id":  accountID,
		"status":      "ok",
	})
}

// POST /admin/v1/providers/{provider}/accounts/{account}/drain
func (s *Server) handleDrainAccount(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	accountID := r.PathValue("account")
	if providerID == "" || accountID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "provider and account path parameters required")
		return
	}

	ctx := r.Context()
	now := time.Now().UTC()

	existing, _ := s.Store.GetAccountOpState(ctx, providerID, accountID)
	enabled := true
	meta := map[string]any{}
	if existing != nil {
		enabled = existing.Enabled
		meta = existing.Metadata
	}

	err := s.Store.UpsertAccountOpState(ctx, storage.AccountOpState{
		ProviderID: providerID,
		AccountID:  accountID,
		Enabled:    enabled,
		Draining:   true,
		UpdatedAt:  now,
		Metadata:   meta,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "drain_error", err.Error())
		return
	}

	_ = s.Store.InsertQuotaEvent(ctx, storage.QuotaEventRecord{
		EventType:  "account_drained",
		ProviderID: providerID,
		AccountID:  accountID,
		Source:     "console",
		Message:    "Account set to drain mode",
		CreatedAt:  now,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          accountID,
		"provider_id": providerID,
		"draining":    true,
		"status":      "draining",
	})
}

// POST /admin/v1/providers/{provider}/accounts/{account}/resume
func (s *Server) handleResumeAccount(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	accountID := r.PathValue("account")
	if providerID == "" || accountID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "provider and account path parameters required")
		return
	}

	ctx := r.Context()
	now := time.Now().UTC()

	existing, _ := s.Store.GetAccountOpState(ctx, providerID, accountID)
	enabled := true
	meta := map[string]any{}
	if existing != nil {
		enabled = existing.Enabled
		meta = existing.Metadata
	}

	err := s.Store.UpsertAccountOpState(ctx, storage.AccountOpState{
		ProviderID: providerID,
		AccountID:  accountID,
		Enabled:    enabled,
		Draining:   false,
		UpdatedAt:  now,
		Metadata:   meta,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resume_error", err.Error())
		return
	}

	_ = s.Store.InsertQuotaEvent(ctx, storage.QuotaEventRecord{
		EventType:  "account_resumed",
		ProviderID: providerID,
		AccountID:  accountID,
		Source:     "console",
		Message:    "Account resumed from drain mode",
		CreatedAt:  now,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          accountID,
		"provider_id": providerID,
		"draining":    false,
		"status":      "active",
	})
}
