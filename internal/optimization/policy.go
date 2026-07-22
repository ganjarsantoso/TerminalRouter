package optimization

import (
	"github.com/termrouter/termrouter/internal/config"
)

// ResolveMode applies the optimization policy precedence:
//
//	server maximum (config default_mode + aggressive_allowed)
//	  > client-key maximum allowed mode
//	  > route policy (not yet separately configured; passes through)
//	  > client request preference
//	  > automatic planner recommendation (safe)
//
// It returns the requested mode (client preference normalized, else server
// default) and the applied mode (the most aggressive allowed by every layer).
// If the applied mode is off, the caller should bypass transformation.
func ResolveMode(cfg config.OptimizationConfig, clientPreference, keyMaxMode string) (requested, applied config.OptimizationMode, err error) {
	serverDefault, err := config.ParseOptimizationMode(string(cfg.DefaultMode))
	if err != nil {
		serverDefault = config.OptModeSafe
	}
	serverMax := serverDefault
	if serverMax == config.OptModeAggressive && !cfg.AggressiveAllowed {
		serverMax = config.OptModeBalanced
	}

	keyMax := serverMax
	if keyMaxMode != "" {
		km, e := config.ParseOptimizationMode(keyMaxMode)
		if e != nil {
			return "", "", e
		}
		if km.Less(serverMax) {
			keyMax = km
		}
	}

	req := serverDefault
	if clientPreference != "" {
		cp, e := config.ParseOptimizationMode(clientPreference)
		if e != nil {
			return "", "", e
		}
		req = cp
	}

	appliedMode := req
	if keyMax.Less(appliedMode) {
		appliedMode = keyMax
	}
	if serverMax.Less(appliedMode) {
		appliedMode = serverMax
	}

	// Planner recommendation is safe; if the resolved mode is more aggressive
	// than the verified-quality envelope supports we would downgrade here. For
	// the deterministic foundation all non-lossy modes are safe, so we keep the
	// resolved mode.
	return req, appliedMode, nil
}
