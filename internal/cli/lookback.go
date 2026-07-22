package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const maxLookbackDays = 365

func ParseLookback(value string) (time.Duration, error) {
	if value == "" {
		return 0, fmt.Errorf("lookback duration must not be empty")
	}

	// Check for day suffix.
	if strings.HasSuffix(value, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid day duration %q", value)
		}
		if n <= 0 {
			return 0, fmt.Errorf("lookback duration must be positive, got %q", value)
		}
		if n > maxLookbackDays {
			return 0, fmt.Errorf("lookback duration %d days exceeds maximum %d days", n, maxLookbackDays)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}

	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", value)
	}
	if d <= 0 {
		return 0, fmt.Errorf("lookback duration must be positive, got %q", value)
	}
	maxDur := time.Duration(maxLookbackDays) * 24 * time.Hour
	if d > maxDur {
		return 0, fmt.Errorf("lookback duration %v exceeds maximum %v", d, maxDur)
	}
	return d, nil
}
