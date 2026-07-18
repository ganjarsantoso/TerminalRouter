package console

import (
	"net/http"
	"strings"
	"time"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/smart"
)

func (s *Server) handleSmartStatus(w http.ResponseWriter, r *http.Request) {
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}
	routes := []map[string]any{}
	for name, rt := range rc.Cfg.Routes {
		if rt.Strategy != "smart" && rt.Smart == nil && len(rt.Candidates) == 0 {
			continue
		}
		mode := "shadow"
		policy := "balanced"
		if rt.Smart != nil {
			if rt.Smart.Mode != "" {
				mode = rt.Smart.Mode
			}
			if rt.Smart.Policy != "" {
				policy = rt.Smart.Policy
			}
		}
		cands := rt.Candidates
		if len(cands) == 0 {
			for _, t := range rt.Targets {
				cands = append(cands, config.CandidateConfig{Provider: t.Provider, Model: t.Model})
			}
		}
		candOut := []map[string]any{}
		for _, c := range cands {
			candOut = append(candOut, map[string]any{
				"provider": c.Provider,
				"model":    c.Model,
				"profile":  c.Profile,
			})
		}
		aliases := []string{}
		for an, a := range rc.Cfg.Aliases {
			if a.Route == name {
				aliases = append(aliases, an)
			}
		}
		routes = append(routes, map[string]any{
			"name":       name,
			"mode":       mode,
			"policy":     policy,
			"candidates": candOut,
			"default":    rt.Default,
			"aliases":    aliases,
			"strategy":   rt.Strategy,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"routes":   routes,
		"policies": []string{"balanced", "quality", "economy", "latency", "privacy"},
	})
}

func (s *Server) handleSmartClassify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "prompt is required")
		return
	}
	task := smart.ClassifyPrompt(body.Prompt)
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

func (s *Server) handleSmartExplain(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prompt string `json:"prompt"`
		Alias  string `json:"alias"`
		Route  string `json:"route"`
		Policy string `json:"policy"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "prompt is required")
		return
	}
	rc, err := s.loadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_error", err.Error())
		return
	}

	routeName := body.Route
	aliasName := body.Alias
	if routeName == "" && aliasName != "" {
		if a, ok := rc.Cfg.Aliases[aliasName]; ok && a.Route != "" {
			routeName = a.Route
		}
	}
	if routeName == "" {
		// Prefer first smart route, or first alias pointing to a smart route.
		for name, rt := range rc.Cfg.Routes {
			if rt.Strategy == "smart" || rt.Smart != nil || len(rt.Candidates) > 0 {
				routeName = name
				break
			}
		}
	}
	if routeName == "" {
		writeError(w, http.StatusBadRequest, "no_smart_route", "no smart route configured")
		return
	}
	route, ok := rc.Cfg.Routes[routeName]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}

	eng := smart.GatewayEngine(rc.Cfg, s.Store, nil)
	reqModel := aliasName
	if reqModel == "" {
		reqModel = routeName
	}
	req := &normalization.NormalizedRequest{
		ID:             "console_explain",
		RequestedModel: reqModel,
		ResolvedAlias:  aliasName,
		Messages: []normalization.Message{{
			Role:    normalization.RoleUser,
			Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: body.Prompt}},
		}},
	}
	routeCfg := smart.RouteFromConfig(routeName, route)
	if routeCfg.Mode == smart.ModeOff {
		routeCfg.Mode = smart.ModeShadow
	}
	if body.Policy != "" {
		routeCfg.Policy = body.Policy
	}
	d, err := eng.Select(req, routeCfg, smart.Override{})
	if err != nil && d == nil {
		writeError(w, http.StatusInternalServerError, "explain_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"decision":    d,
		"explanation": smart.FormatDecision(d),
		"route":       routeName,
	})
}

func (s *Server) handleSmartReports(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-7 * 24 * time.Hour)
	agg, err := s.Store.SmartShadowStats(r.Context(), "", since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "report_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"period": "7d", "report": agg})
}

func (s *Server) handleSmartRouteReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	since := time.Now().Add(-7 * 24 * time.Hour)
	agg, err := s.Store.SmartShadowStats(r.Context(), id, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "report_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"route": id, "period": "7d", "report": agg})
}

func (s *Server) setSmartMode(w http.ResponseWriter, r *http.Request, mode string) {
	id := r.PathValue("id")
	rev, err := s.applyMutation("smart_mode_"+mode, id, func(cfg *config.Config) error {
		rt, ok := cfg.Routes[id]
		if !ok {
			return errNotFound("route")
		}
		if rt.Smart == nil {
			rt.Smart = &config.SmartConfig{}
		}
		rt.Smart.Mode = mode
		if rt.Strategy == "" {
			rt.Strategy = "smart"
		}
		cfg.Routes[id] = rt
		return nil
	})
	if err != nil {
		if _, ok := err.(*consoleError); ok {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "save_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"route": id, "mode": mode, "revision": rev})
}

func (s *Server) handleSmartEnableShadow(w http.ResponseWriter, r *http.Request) {
	s.setSmartMode(w, r, "shadow")
}

func (s *Server) handleSmartEnableLive(w http.ResponseWriter, r *http.Request) {
	// Require explicit confirmation body for live activation.
	var body struct {
		Confirm bool `json:"confirm"`
	}
	_ = decodeJSON(r, &body)
	if !body.Confirm {
		writeError(w, http.StatusBadRequest, "confirmation_required",
			"set confirm:true to enable live Smart Route selection for new requests")
		return
	}
	s.setSmartMode(w, r, "live")
}

func (s *Server) handleSmartDisable(w http.ResponseWriter, r *http.Request) {
	s.setSmartMode(w, r, "off")
}
