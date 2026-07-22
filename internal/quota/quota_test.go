package quota

import (
	"testing"
	"time"
)

func TestComputeBurnRates(t *testing.T) {
	in := BurnRateInput{
		Last15Min:       100,
		LastHour:        400,
		CurrentWindow:   800,
		WindowElapsed:   4 * time.Hour,
		PrevWindowTotal: 1600,
		PrevWindowDur:   24 * time.Hour,
		Last7Days:       10000,
	}

	rates := ComputeBurnRates(in)

	if rates.Last15Min != 400 {
		t.Errorf("Last15Min = %v, want 400", rates.Last15Min)
	}
	if rates.LastHour != 400 {
		t.Errorf("LastHour = %v, want 400", rates.LastHour)
	}
	if rates.CurrentWindow != 200 {
		t.Errorf("CurrentWindow = %v, want 200", rates.CurrentWindow)
	}
	if rates.PrevWindow != 66.66666666666667 {
		t.Errorf("PrevWindow = %v, want ~66.67", rates.PrevWindow)
	}
	// Prefer recent non-zero rate
	if rates.Selected != rates.Last15Min {
		t.Errorf("Selected = %v, want Last15Min %v", rates.Selected, rates.Last15Min)
	}
}

func TestForecastExhaustion(t *testing.T) {
	limit := 1000.0
	resetAt := time.Now().UTC().Add(1 * time.Hour)

	// Will exhaust before reset.
	in := ForecastInput{
		Limit:    &limit,
		Used:     800,
		Reserved: 0,
		BurnRate: 300, // 300/hour, 200 remaining -> exhausts in 40min
		Now:      time.Now().UTC(),
		ResetAt:  &resetAt,
	}

	result := ForecastExhaustion(in)
	if result.Status != ForecastLikelyExhaust {
		t.Errorf("Status = %v, want likely_to_exhaust_before_reset", result.Status)
	}
	if result.ForecastExhaustAt == nil {
		t.Fatal("ForecastExhaustAt should not be nil")
	}

	// Safe through reset.
	in.Used = 100
	in.BurnRate = 50 // 900 remaining -> 18 hours -> safe through reset (1 hour)
	result = ForecastExhaustion(in)
	if result.Status != ForecastSafeThroughReset {
		t.Errorf("Status = %v, want safe_through_reset", result.Status)
	}
}

func TestForecastExhaustion_UnknownLimit(t *testing.T) {
	in := ForecastInput{
		Limit:    nil,
		Used:     100,
		BurnRate: 50,
		Now:      time.Now().UTC(),
	}
	result := ForecastExhaustion(in)
	if result.Status != ForecastInsufficientData {
		t.Errorf("Status = %v, want insufficient_data for nil limit", result.Status)
	}
}

func TestForecastExhaustion_ZeroBurnRate(t *testing.T) {
	limit := 1000.0
	resetAt := time.Now().UTC().Add(1 * time.Hour)
	in := ForecastInput{
		Limit:    &limit,
		Used:     500,
		BurnRate: 0,
		Now:      time.Now().UTC(),
		ResetAt:  &resetAt,
	}
	result := ForecastExhaustion(in)
	if result.Status != ForecastSafeThroughReset {
		t.Errorf("Status = %v, want safe_through_reset for zero burn rate", result.Status)
	}
}

func TestDeriveStatus(t *testing.T) {
	now := time.Now().UTC()
	staleAfter := now.Add(5 * time.Minute)

	util05 := 0.5
	util08 := 0.8
	util095 := 0.95
	util10 := 1.0

	tests := []struct {
		name      string
		util      *float64
		forecast  ForecastStatus
		stale     time.Time
		limitKnow bool
		want      QuotaStatus
	}{
		{"healthy_low_util", &util05, ForecastSafeThroughReset, staleAfter, true, StatusWatch}, // 0.5 >= watch threshold
		{"watch_forecast", &util05, ForecastLikelyExhaust, staleAfter, true, StatusWatch},      // 0.5 >= watch threshold, checked before forecast
		{"warning", &util08, ForecastSafeThroughReset, staleAfter, true, StatusWarning},
		{"critical", &util095, ForecastSafeThroughReset, staleAfter, true, StatusCritical},
		{"exhausted_util", &util10, ForecastSafeThroughReset, staleAfter, true, StatusExhausted},
		{"exhausted_forecast", nil, ForecastAlreadyExhausted, staleAfter, true, StatusExhausted},
		{"stale", &util05, ForecastSafeThroughReset, now.Add(-1 * time.Minute), true, StatusStale},
		{"unknown_limit", nil, ForecastInsufficientData, staleAfter, false, StatusUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveStatus(tt.util, tt.forecast, now, tt.stale, tt.limitKnow)
			if got != tt.want {
				t.Errorf("DeriveStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUtilization(t *testing.T) {
	limit := 1000.0
	util := Utilization(&limit, 500)
	if util == nil || *util != 0.5 {
		t.Errorf("Utilization = %v, want 0.5", util)
	}

	// Unknown limit
	util = Utilization(nil, 500)
	if util != nil {
		t.Errorf("Utilization = %v, want nil for unknown limit", util)
	}
}

func TestRemaining(t *testing.T) {
	limit := 1000.0
	rem := Remaining(&limit, 300, 100)
	if rem == nil || *rem != 600 {
		t.Errorf("Remaining = %v, want 600", rem)
	}

	// Negative remaining clamped to 0
	rem = Remaining(&limit, 900, 200)
	if rem == nil || *rem != 0 {
		t.Errorf("Remaining = %v, want 0 (clamped)", rem)
	}
}

func TestWindowBounds_Daily(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	now := time.Date(2026, 7, 21, 10, 30, 0, 0, loc) // 10:30 AM ET

	rule := ResetRule{
		Type:     "daily",
		Hour:     0,
		Minute:   0,
		Timezone: "America/New_York",
	}

	start, end, resetAt, wtype, err := WindowBounds(rule, now)
	if err != nil {
		t.Fatal(err)
	}
	if wtype != WindowFixedCalendar {
		t.Errorf("WindowType = %v, want fixed_calendar", wtype)
	}
	if resetAt == nil {
		t.Fatal("ResetAt should not be nil")
	}
	// Reset should be at midnight ET (next day) = 04:00 UTC
	if resetAt.Hour() != 4 || resetAt.Minute() != 0 {
		t.Errorf("ResetAt = %v, want 04:00 UTC (midnight ET)", resetAt)
	}
	// Window should start 24h before reset
	if start.After(now) {
		t.Errorf("WindowStart = %v should be before now %v", start, now)
	}
	_ = end
}

func TestWindowBounds_Rolling(t *testing.T) {
	now := time.Now().UTC()
	rule := ResetRule{
		Type:     "rolling",
		Duration: 5 * time.Hour,
	}

	start, _, resetAt, wtype, err := WindowBounds(rule, now)
	if err != nil {
		t.Fatal(err)
	}
	if wtype != WindowRolling {
		t.Errorf("WindowType = %v, want rolling", wtype)
	}
	if resetAt == nil {
		t.Fatal("ResetAt should not be nil")
	}
	expectedStart := now.Add(-5 * time.Hour)
	if start.Sub(expectedStart) > time.Second {
		t.Errorf("WindowStart = %v, want ~%v", start, expectedStart)
	}
}

func TestIdealUtilization(t *testing.T) {
	start := time.Now().UTC().Add(-6 * time.Hour)
	end := start.Add(24 * time.Hour)
	now := start.Add(12 * time.Hour) // 50% elapsed

	ideal := IdealUtilization(start, &end, now)
	if ideal < 0.49 || ideal > 0.51 {
		t.Errorf("IdealUtilization = %v, want ~0.5", ideal)
	}
}

func TestPaceLabel(t *testing.T) {
	underPace := -0.3
	onPace := 0.0
	aheadPace := 0.3
	util05 := 0.5
	util10 := 1.0

	if PaceLabel(&underPace, &util05) != "underutilized" {
		t.Error("expected underutilized")
	}
	if PaceLabel(&onPace, &util05) != "on_pace" {
		t.Error("expected on_pace")
	}
	if PaceLabel(&aheadPace, &util05) != "ahead_of_pace" {
		t.Error("expected ahead_of_pace")
	}
	if PaceLabel(&aheadPace, &util10) != "likely_to_exhaust_early" {
		t.Error("expected likely_to_exhaust_early")
	}
}

func TestDefaultThresholds(t *testing.T) {
	th := DefaultThresholds()
	if th.Watch != 0.50 || th.Warning != 0.75 || th.Critical != 0.90 {
		t.Errorf("thresholds = %v, want {0.50, 0.75, 0.90}", th)
	}
}

func TestReconcile(t *testing.T) {
	cfg := DefaultReconcileConfig()
	now := time.Now().UTC()

	// No drift
	r := Reconcile(100, 100, now, now, cfg)
	if r.MaterialDrift {
		t.Error("should not be material drift when equal")
	}

	// Provider > local by 20%
	r = Reconcile(1000, 800, now, now, cfg)
	if !r.MaterialDrift {
		t.Error("should be material drift at 20%")
	}

	// Small drift within threshold
	r = Reconcile(1000, 920, now, now, cfg) // 8% drift
	if r.MaterialDrift {
		t.Error("should not be material drift at 8%")
	}
}
