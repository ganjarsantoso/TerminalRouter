package cli

import (
	"testing"
	"time"
)

func TestParseLookback_1d(t *testing.T) {
	d, err := ParseLookback("1d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 24*time.Hour {
		t.Fatalf("expected 24h, got %v", d)
	}
}

func TestParseLookback_7d(t *testing.T) {
	d, err := ParseLookback("7d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 168*time.Hour {
		t.Fatalf("expected 168h, got %v", d)
	}
}

func TestParseLookback_30m(t *testing.T) {
	d, err := ParseLookback("30m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 30*time.Minute {
		t.Fatalf("expected 30m, got %v", d)
	}
}

func TestParseLookback_0d(t *testing.T) {
	_, err := ParseLookback("0d")
	if err == nil {
		t.Fatal("expected error for zero duration")
	}
}

func TestParseLookback_Negative(t *testing.T) {
	_, err := ParseLookback("-1h")
	if err == nil {
		t.Fatal("expected error for negative duration")
	}
}

func TestParseLookback_ExceedsMax(t *testing.T) {
	_, err := ParseLookback("366d")
	if err == nil {
		t.Fatal("expected error for exceeding max lookback")
	}
}

func TestParseLookback_InvalidFormat(t *testing.T) {
	_, err := ParseLookback("not-a-duration")
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}
