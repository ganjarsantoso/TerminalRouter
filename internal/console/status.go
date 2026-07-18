package console

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/storage"
)

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	health, _ := s.Store.ListProviderHealth(r.Context())
	healthMap := map[string]any{}
	healthyCount := 0
	for _, h := range health {
		state := string(h.CircuitState)
		if state == "" || state == string(storage.CircuitClosed) {
			state = "healthy"
			healthyCount++
		} else if state == string(storage.CircuitOpen) {
			state = "cooldown"
		}
		entry := map[string]any{
			"circuit":      h.CircuitState,
			"state":        state,
			"failures":     h.ConsecutiveFailures,
			"last_error":   h.LastError,
			"latency_ms":   h.LastLatencyMs,
			"last_success": h.LastSuccessAt,
		}
		if h.CooldownUntil != nil {
			entry["cooldown_until"] = h.CooldownUntil.UTC().Format(time.RFC3339)
			entry["cooldown_remaining_s"] = int(time.Until(*h.CooldownUntil).Seconds())
		}
		healthMap[h.ProviderID] = entry
	}
	// Providers with no health row are treated as unknown/healthy by default.
	for name := range rc.Cfg.Providers {
		if _, ok := healthMap[name]; !ok {
			healthMap[name] = map[string]any{"state": "healthy", "circuit": storage.CircuitClosed, "latency_ms": 0}
			healthyCount++
		}
	}

	running := false
	activeStreams := int64(0)
	uptime := ""
	if s.App != nil {
		st := s.App.RuntimeStatus()
		running = st.Running
		activeStreams = st.ActiveStreams
		uptime = st.Uptime
	}

	smartModes := map[string]string{}
	shadowCount, liveCount := 0, 0
	for name, rt := range rc.Cfg.Routes {
		if rt.Strategy == "smart" || rt.Smart != nil || len(rt.Candidates) > 0 {
			mode := "shadow"
			if rt.Smart != nil && rt.Smart.Mode != "" {
				mode = rt.Smart.Mode
			}
			smartModes[name] = mode
			switch mode {
			case "live":
				liveCount++
			case "shadow":
				shadowCount++
			}
		}
	}

	since := time.Now().UTC().Truncate(24 * time.Hour)
	usage, _ := s.Store.UsageSince(r.Context(), since)

	providerTotal := len(rc.Cfg.Providers)
	routingHealthyPct := 100
	if providerTotal > 0 {
		routingHealthyPct = int(float64(healthyCount) / float64(providerTotal) * 100)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"gateway": map[string]any{
			"address":         rc.Cfg.Addr(),
			"running":         running,
			"auth":            rc.Cfg.Server.AuthRequired,
			"uptime":          uptime,
			"active_streams":  activeStreams,
			"max_concurrency": rc.Cfg.Server.MaxConcurrency,
		},
		"console": map[string]any{
			"address": fmtSprintf("http://%s:%d", s.Host, s.Port),
			"mode":    "local only",
			"uptime":  time.Since(s.started).Round(time.Second).String(),
		},
		"revision":           rc.Revision,
		"providers":          providerSummaries(rc.Cfg, healthMap),
		"provider_health":    healthMap,
		"aliases":            rc.Cfg.Aliases,
		"routes":             rc.Cfg.Routes,
		"smart_modes":        smartModes,
		"smart_shadow_count": shadowCount,
		"smart_live_count":   liveCount,
		"initialized":        s.isInitialized(),
		"credential_backend": rc.Cfg.Credentials.Backend,
		"requests_today":     usage.TotalRequests,
		"usage_today":        usage,
		"routing_slice": map[string]any{
			"healthy_pct": routingHealthyPct,
			"healthy":     healthyCount,
			"total":       providerTotal,
		},
		"home": s.Home,
	})
}

func providerSummaries(cfg *config.Config, healthMap map[string]any) []map[string]any {
	out := []map[string]any{}
	for name, p := range cfg.Providers {
		entry := map[string]any{
			"name":     name,
			"type":     p.Type,
			"base_url": p.BaseURL,
			"enabled":  p.IsEnabled(),
			"model":    "",
			"state":    "healthy",
		}
		if h, ok := healthMap[name].(map[string]any); ok {
			if st, ok := h["state"].(string); ok {
				entry["state"] = st
			}
			if lat, ok := h["latency_ms"]; ok {
				entry["latency_ms"] = lat
			}
			if cd, ok := h["cooldown_remaining_s"]; ok {
				entry["cooldown_remaining_s"] = cd
			}
		}
		out = append(out, entry)
	}
	return out
}

func (s *Server) isInitialized() bool {
	if _, err := os.Stat(s.Paths.Config); err != nil {
		return false
	}
	return true
}

type diagCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | error
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
	CLI    string `json:"cli,omitempty"`
	Safe   bool   `json:"safe_fix"`
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	checks := s.runDiagnostics(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"checks": checks})
}

func (s *Server) handleDiagnosticsRun(w http.ResponseWriter, r *http.Request) {
	checks := s.runDiagnostics(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"checks": checks, "run_at": time.Now().UTC()})
}

func (s *Server) runDiagnostics(ctx context.Context) []diagCheck {
	checks := []diagCheck{}
	if _, err := os.Stat(s.Paths.Config); err != nil {
		checks = append(checks, diagCheck{Name: "config.yaml present", Status: "error", Detail: "configuration file missing", Fix: "Initialize TermRouter", CLI: "termrouter init", Safe: true})
	} else {
		cfg, err := config.Load(s.Paths.Config)
		if err != nil {
			checks = append(checks, diagCheck{Name: "config.yaml valid", Status: "error", Detail: err.Error(), Fix: "Fix configuration", CLI: "termrouter config check", Safe: false})
		} else {
			checks = append(checks, diagCheck{Name: "config.yaml valid", Status: "ok", Detail: "valid configuration", Safe: true})
			if !cfg.Server.AuthRequired {
				checks = append(checks, diagCheck{Name: "auth_required", Status: "warn", Detail: "auth_required is false", Fix: "Enable authentication", CLI: "termrouter config", Safe: false})
			}
			if len(cfg.Providers) == 0 {
				checks = append(checks, diagCheck{Name: "providers configured", Status: "warn", Detail: "no providers configured", Fix: "Add a provider", CLI: "termrouter provider add", Safe: true})
			}
			if len(cfg.Aliases) == 0 && len(cfg.Routes) == 0 {
				checks = append(checks, diagCheck{Name: "routes/aliases configured", Status: "warn", Detail: "no aliases or routes yet", Fix: "Add an alias or route", CLI: "termrouter alias add", Safe: true})
			}
		}
	}
	if _, err := os.Stat(s.Paths.Database); err != nil {
		checks = append(checks, diagCheck{Name: "database present", Status: "error", Detail: "router.db missing", Fix: "Initialize TermRouter", CLI: "termrouter init", Safe: true})
	} else {
		keys, err := s.Store.ListClientKeys(ctx)
		if err != nil {
			checks = append(checks, diagCheck{Name: "database open", Status: "error", Detail: err.Error(), Safe: false})
		} else {
			checks = append(checks, diagCheck{Name: "database open", Status: "ok", Detail: "database OK", Safe: true})
			if len(keys) == 0 {
				checks = append(checks, diagCheck{Name: "client keys", Status: "warn", Detail: "no client keys configured", Fix: "Create a client key", CLI: "termrouter key create", Safe: true})
			}
		}
	}
	for _, d := range []string{s.Paths.LogsDir, s.Paths.RunDir} {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			checks = append(checks, diagCheck{Name: "dir " + filepath.Base(d), Status: "error", Detail: "missing directory", Fix: "Create directory", CLI: "termrouter init", Safe: true})
		} else {
			checks = append(checks, diagCheck{Name: "dir " + filepath.Base(d), Status: "ok", Detail: "present", Safe: true})
		}
	}
	checks = append(checks, diagCheck{Name: "console loopback binding", Status: "ok", Detail: s.Host + ":" + itoa(s.Port), Safe: true})
	rc, err := s.loadConfig()
	if err == nil {
		for name, p := range rc.Cfg.Providers {
			if p.CredentialRef == "" || p.CredentialRef == "none://" {
				continue
			}
			if s.Creds != nil {
				if _, rerr := s.Creds.Resolve(p.CredentialRef); rerr != nil {
					checks = append(checks, diagCheck{Name: "credential " + name, Status: "error", Detail: "cannot resolve " + redactRef(p.CredentialRef) + ": " + rerr.Error(), Fix: "Update credential", CLI: "termrouter provider test " + name, Safe: false})
				} else {
					checks = append(checks, diagCheck{Name: "credential " + name, Status: "ok", Detail: "available", Safe: true})
				}
			}
		}
	}
	return checks
}
