package console

import (
	"context"

	"github.com/termrouter/termrouter/internal/config"
	"gopkg.in/yaml.v3"
)

// revisionedConfig bundles the effective config with its revision number.
type revisionedConfig struct {
	Cfg      *config.Config
	Revision int64
}

// loadConfig reads config and current revision.
func (s *Server) loadConfig() (*revisionedConfig, error) {
	cfg, err := config.Load(s.Paths.Config)
	if err != nil {
		return nil, err
	}
	ctx := s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	rev, err := s.Store.GetLatestConfigRevision(ctx)
	if err != nil {
		return nil, err
	}
	return &revisionedConfig{Cfg: cfg, Revision: rev}, nil
}

// applyMutation validates, persists, records history and (optionally) reloads the runtime.
// The change closure mutates cfg in place. It returns the new revision.
func (s *Server) applyMutation(changeType, resources string, change func(cfg *config.Config) error) (int64, error) {
	rc, err := s.loadConfig()
	if err != nil {
		return 0, err
	}
	cfg := rc.Cfg
	if change != nil {
		if err := change(cfg); err != nil {
			return 0, err
		}
	}
	if err := cfg.Validate(); err != nil {
		return 0, err
	}
	if err := config.Save(s.Paths.Config, cfg); err != nil {
		return 0, err
	}
	rev := rc.Revision + 1
	raw, _ := yaml.Marshal(cfg)
	san, _ := yaml.Marshal(cfg.ExportSanitized())
	ctx := s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	stored, err := s.Store.InsertConfigHistory(ctx, s.sessionID(), changeType, resources, string(raw), string(san))
	if err == nil {
		rev = stored
	}
	if s.App != nil && s.ReloadRuntime {
		_ = s.App.Reload(cfg)
	}
	return rev, nil
}

// sessionID returns the current admin session id (best-effort, no-op default).
func (s *Server) sessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentSession
}
