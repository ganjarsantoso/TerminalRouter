package quota

import "time"

// StatusThresholds control utilization → status mapping.
type StatusThresholds struct {
	Watch    float64 // default 0.50
	Warning  float64 // default 0.75
	Critical float64 // default 0.90
}

// DefaultThresholds returns conservative utilization thresholds.
func DefaultThresholds() StatusThresholds {
	return StatusThresholds{Watch: 0.50, Warning: 0.75, Critical: 0.90}
}

// DeriveStatus computes quota status from utilization, forecast, and freshness.
func DeriveStatus(util *float64, forecast ForecastStatus, now time.Time, staleAfter time.Time, limitKnown bool) QuotaStatus {
	if !staleAfter.IsZero() && now.After(staleAfter) {
		return StatusStale
	}
	if !limitKnown {
		return StatusUnknown
	}
	if forecast == ForecastAlreadyExhausted {
		return StatusExhausted
	}
	if util != nil {
		if *util >= 1 {
			return StatusExhausted
		}
		th := DefaultThresholds()
		switch {
		case *util >= th.Critical:
			return StatusCritical
		case *util >= th.Warning:
			return StatusWarning
		case *util >= th.Watch:
			return StatusWatch
		}
	}
	if forecast == ForecastLikelyExhaust {
		return StatusWarning
	}
	return StatusHealthy
}

// IsStale reports whether a snapshot has passed its stale-after timestamp.
func IsStale(now, staleAfter time.Time) bool {
	return !staleAfter.IsZero() && now.After(staleAfter)
}

// DefaultStaleAfter returns now + duration based on health.
func DefaultStaleAfter(now time.Time, status QuotaStatus) time.Time {
	switch status {
	case StatusCritical, StatusExhausted:
		return now.Add(2 * time.Minute)
	case StatusWarning, StatusWatch:
		return now.Add(5 * time.Minute)
	default:
		return now.Add(15 * time.Minute)
	}
}
