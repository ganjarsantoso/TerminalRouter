package quota

import (
	"fmt"
	"time"
)

// WindowBounds computes the current window start/end and next reset for a rule.
// All returned times are UTC. When the reset cannot be determined, resetAt is nil
// and windowType is WindowUnknown.
func WindowBounds(rule ResetRule, now time.Time) (start time.Time, end *time.Time, resetAt *time.Time, wtype WindowType, err error) {
	now = now.UTC()
	tz := rule.Timezone
	if tz == "" {
		tz = "UTC"
	}
	loc, locErr := time.LoadLocation(tz)
	if locErr != nil {
		return time.Time{}, nil, nil, WindowUnknown, fmt.Errorf("invalid timezone %q: %w", tz, locErr)
	}
	local := now.In(loc)

	switch rule.Type {
	case "rolling":
		dur := rule.Duration
		if dur <= 0 {
			return time.Time{}, nil, nil, WindowUnknown, fmt.Errorf("rolling reset requires positive duration")
		}
		start = now.Add(-dur)
		r := now.Add(dur) // next full-window horizon from now for display
		// For rolling windows the "reset" is continuous; expose window end as now+remaining
		// of the current observation horizon.
		end = TimePtr(now)
		// Rolling windows do not have a calendar reset; callers use duration horizon.
		resetAt = TimePtr(r)
		return start, end, resetAt, WindowRolling, nil

	case "daily", "fixed_daily":
		// Next reset at Hour:Minute local time.
		candidate := time.Date(local.Year(), local.Month(), local.Day(), rule.Hour, rule.Minute, 0, 0, loc)
		if !local.Before(candidate) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		startLocal := candidate.AddDate(0, 0, -1)
		start = startLocal.UTC()
		endT := candidate.UTC()
		end = &endT
		resetAt = &endT
		return start, end, resetAt, WindowFixedCalendar, nil

	case "weekly":
		dow := time.Monday
		if rule.DayOfWeek != nil {
			dow = *rule.DayOfWeek
		}
		// Find next occurrence of dow at Hour:Minute.
		daysAhead := (int(dow) - int(local.Weekday()) + 7) % 7
		candidate := time.Date(local.Year(), local.Month(), local.Day(), rule.Hour, rule.Minute, 0, 0, loc).AddDate(0, 0, daysAhead)
		if !local.Before(candidate) {
			candidate = candidate.AddDate(0, 0, 7)
		}
		startLocal := candidate.AddDate(0, 0, -7)
		start = startLocal.UTC()
		endT := candidate.UTC()
		end = &endT
		resetAt = &endT
		return start, end, resetAt, WindowFixedCalendar, nil

	case "monthly", "billing_cycle":
		day := rule.DayOfMonth
		if day <= 0 {
			day = 1
		}
		if rule.Anchor != nil && !rule.Anchor.IsZero() {
			// Align to anchor day-of-month when provided.
			day = rule.Anchor.In(loc).Day()
		}
		// Clamp day to month length.
		year, month := local.Year(), local.Month()
		candidate := clampDay(year, month, day, rule.Hour, rule.Minute, loc)
		if !local.Before(candidate) {
			// next month
			nm := month + 1
			ny := year
			if nm > 12 {
				nm = 1
				ny++
			}
			candidate = clampDay(ny, nm, day, rule.Hour, rule.Minute, loc)
		}
		// Start is previous cycle boundary.
		pm := candidate.Month() - 1
		py := candidate.Year()
		if pm < 1 {
			pm = 12
			py--
		}
		startLocal := clampDay(py, pm, day, rule.Hour, rule.Minute, loc)
		start = startLocal.UTC()
		endT := candidate.UTC()
		end = &endT
		resetAt = &endT
		return start, end, resetAt, WindowFixedCalendar, nil

	case "provider_reported":
		// Bounds unknown without provider data; return unknown.
		return now, nil, nil, WindowProviderReported, nil

	case "lifetime":
		start = time.Unix(0, 0).UTC()
		return start, nil, nil, WindowLifetime, nil

	case "unknown", "":
		return now, nil, nil, WindowUnknown, nil

	default:
		return time.Time{}, nil, nil, WindowUnknown, fmt.Errorf("unsupported reset rule type %q", rule.Type)
	}
}

func clampDay(year int, month time.Month, day, hour, minute int, loc *time.Location) time.Time {
	// Find last day of month.
	firstNext := time.Date(year, month+1, 1, 0, 0, 0, 0, loc)
	lastDay := firstNext.AddDate(0, 0, -1).Day()
	if day > lastDay {
		day = lastDay
	}
	if day < 1 {
		day = 1
	}
	return time.Date(year, month, day, hour, minute, 0, 0, loc)
}

// IdealUtilization returns elapsed_window / total_window for pace guidance.
// Returns 0 when bounds are unknown.
func IdealUtilization(windowStart time.Time, windowEnd *time.Time, now time.Time) float64 {
	if windowEnd == nil || windowEnd.IsZero() || windowStart.IsZero() {
		return 0
	}
	total := windowEnd.Sub(windowStart)
	if total <= 0 {
		return 0
	}
	elapsed := now.Sub(windowStart)
	if elapsed < 0 {
		return 0
	}
	if elapsed > total {
		return 1
	}
	return float64(elapsed) / float64(total)
}

// PaceLabel classifies pace delta for subscription guidance.
func PaceLabel(paceDelta *float64, utilization *float64) string {
	if paceDelta == nil || utilization == nil {
		return "unknown"
	}
	if *utilization >= 1 {
		return "likely_to_exhaust_early"
	}
	// Ahead of pace with substantial remaining time → risk of early exhaust.
	if *paceDelta > 0.15 {
		return "ahead_of_pace"
	}
	if *paceDelta < -0.15 {
		return "underutilized"
	}
	return "on_pace"
}
