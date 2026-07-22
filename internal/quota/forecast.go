package quota

import "time"

// Default confidence threshold below which forecasts are suppressed.
const DefaultForecastConfidence = 0.4

// BurnRateInput carries usage samples for multi-window burn-rate calculation.
type BurnRateInput struct {
	// Units consumed in each lookback window (must be non-negative).
	Last15Min float64
	LastHour  float64
	// Units consumed so far in the current quota window.
	CurrentWindow float64
	// Elapsed time of the current window.
	WindowElapsed time.Duration
	// Comparable completed previous window total (optional).
	PrevWindowTotal float64
	PrevWindowDur   time.Duration
	// Seven-day total.
	Last7Days float64
}

// BurnRates holds multiple burn rates expressed as units per hour.
type BurnRates struct {
	Last15Min     float64
	LastHour      float64
	CurrentWindow float64
	PrevWindow    float64
	SevenDayAvg   float64
	// Selected is the rate used for forecasting (prefer recent non-zero).
	Selected      float64
	SelectedLabel string
}

// ComputeBurnRates returns units-per-hour rates and picks a preferred rate.
func ComputeBurnRates(in BurnRateInput) BurnRates {
	var out BurnRates
	if in.Last15Min > 0 {
		out.Last15Min = in.Last15Min / 0.25 // 15 min = 0.25 h
	}
	if in.LastHour > 0 {
		out.LastHour = in.LastHour
	}
	if in.WindowElapsed > 0 && in.CurrentWindow > 0 {
		out.CurrentWindow = in.CurrentWindow / in.WindowElapsed.Hours()
	}
	if in.PrevWindowDur > 0 && in.PrevWindowTotal > 0 {
		out.PrevWindow = in.PrevWindowTotal / in.PrevWindowDur.Hours()
	}
	if in.Last7Days > 0 {
		out.SevenDayAvg = in.Last7Days / (7 * 24)
	}

	// Prefer recent non-zero rates for exhaustion risk.
	switch {
	case out.Last15Min > 0:
		out.Selected = out.Last15Min
		out.SelectedLabel = "last_15m"
	case out.LastHour > 0:
		out.Selected = out.LastHour
		out.SelectedLabel = "last_hour"
	case out.CurrentWindow > 0:
		out.Selected = out.CurrentWindow
		out.SelectedLabel = "current_window"
	case out.SevenDayAvg > 0:
		out.Selected = out.SevenDayAvg
		out.SelectedLabel = "seven_day_avg"
	case out.PrevWindow > 0:
		out.Selected = out.PrevWindow
		out.SelectedLabel = "prev_window"
	default:
		out.Selected = 0
		out.SelectedLabel = "none"
	}
	return out
}

// ForecastInput controls exhaustion forecasting.
type ForecastInput struct {
	Limit      *float64
	Used       float64
	Reserved   float64
	BurnRate   float64 // units per hour
	Now        time.Time
	ResetAt    *time.Time
	Stale      bool
	Confidence float64
	// MinConfidence defaults to DefaultForecastConfidence when zero.
	MinConfidence float64
}

// ForecastResult is the outcome of an exhaustion forecast.
type ForecastResult struct {
	Remaining         *float64
	ForecastExhaustAt *time.Time
	Status            ForecastStatus
}

// ForecastExhaustion computes remaining and forecast status.
// Does not invent a forecast when burn rate is zero, limit unknown, data stale,
// or confidence is below threshold.
func ForecastExhaustion(in ForecastInput) ForecastResult {
	out := ForecastResult{Status: ForecastInsufficientData}
	if in.Limit == nil || *in.Limit <= 0 {
		return out
	}
	rem := *in.Limit - in.Used - in.Reserved
	if rem < 0 {
		rem = 0
	}
	out.Remaining = &rem

	if rem <= 0 {
		out.Status = ForecastAlreadyExhausted
		return out
	}
	if in.Stale {
		out.Status = ForecastProviderStale
		return out
	}
	minConf := in.MinConfidence
	if minConf <= 0 {
		minConf = DefaultForecastConfidence
	}
	if in.Confidence > 0 && in.Confidence < minConf {
		return out
	}
	if in.BurnRate <= 0 {
		// Zero burn: safe through reset if we know reset, else insufficient.
		if in.ResetAt != nil && !in.ResetAt.IsZero() {
			out.Status = ForecastSafeThroughReset
		}
		return out
	}

	hours := rem / in.BurnRate
	exhaust := in.Now.UTC().Add(time.Duration(hours * float64(time.Hour)))
	out.ForecastExhaustAt = &exhaust

	if in.ResetAt != nil && !in.ResetAt.IsZero() {
		if exhaust.Before(*in.ResetAt) {
			out.Status = ForecastLikelyExhaust
		} else {
			out.Status = ForecastSafeThroughReset
		}
		return out
	}
	// No reset known — still report exhaust time with insufficient_data-ish status
	// only when we cannot compare to reset. PRD: suppress when rolling behavior unknown.
	out.Status = ForecastInsufficientData
	out.ForecastExhaustAt = nil
	return out
}

// Remaining computes max(limit - used - reserved, 0). Returns nil when limit unknown.
func Remaining(limit *float64, used, reserved float64) *float64 {
	if limit == nil || *limit <= 0 {
		return nil
	}
	r := *limit - used - reserved
	if r < 0 {
		r = 0
	}
	return &r
}

// Utilization computes used/limit. Returns nil when limit unknown or non-positive.
func Utilization(limit *float64, used float64) *float64 {
	if limit == nil || *limit <= 0 {
		return nil
	}
	u := used / *limit
	return &u
}
