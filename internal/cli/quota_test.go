package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/termrouter/termrouter/internal/storage"
)

func TestSnapshotRecordsToAny(t *testing.T) {
	now := time.Now().UTC()
	snaps := []storage.QuotaSnapshotRecord{
		{
			ProviderID: "openai",
			AccountID:  "default",
			Dimension:  "requests",
			UsedValue:  50,
			LimitValue: ptr(100.0),
			Source:     "local_authoritative",
			Confidence: 1.0,
			ObservedAt: now,
		},
		{
			ProviderID: "anthropic",
			AccountID:  "default",
			Dimension:  "tokens",
			UsedValue:  900,
			LimitValue: ptr(1000.0),
			Source:     "provider_reported",
			Confidence: 0.8,
			ObservedAt: now.Add(-10 * time.Minute),
		},
	}
	items := snapshotRecordsToAny(snaps)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	m1 := items[0].(map[string]any)
	if m1["provider_id"] != "openai" {
		t.Fatalf("expected openai, got %v", m1["provider_id"])
	}
	if m1["dimension"] != "requests" {
		t.Fatalf("expected requests, got %v", m1["dimension"])
	}
	if m1["limit"].(float64) != 100 {
		t.Fatalf("expected limit 100, got %v", m1["limit"])
	}
	if m1["utilization"].(float64) != 0.5 {
		t.Fatalf("expected utilization 0.5, got %v", m1["utilization"])
	}

	m2 := items[1].(map[string]any)
	if m2["provider_id"] != "anthropic" {
		t.Fatalf("expected anthropic, got %v", m2["provider_id"])
	}
	if m2["utilization"].(float64) != 0.9 {
		t.Fatalf("expected utilization 0.9, got %v", m2["utilization"])
	}
}

func TestSnapshotRecordsToAny_NoLimit(t *testing.T) {
	snaps := []storage.QuotaSnapshotRecord{
		{
			ProviderID: "openai",
			AccountID:  "default",
			Dimension:  "requests",
			UsedValue:  50,
			Source:     "local_authoritative",
			Confidence: 1.0,
			ObservedAt: time.Now().UTC(),
		},
	}
	items := snapshotRecordsToAny(snaps)
	m := items[0].(map[string]any)
	if m["limit"] != nil {
		t.Fatal("expected nil limit")
	}
	if m["utilization"] != nil {
		t.Fatal("expected nil utilization when limit is nil")
	}
}

func TestBuildAlerts_None(t *testing.T) {
	now := time.Now().UTC()
	snaps := []storage.QuotaSnapshotRecord{
		{
			ProviderID: "openai",
			AccountID:  "default",
			Dimension:  "requests",
			UsedValue:  10,
			LimitValue: ptr(100.0),
			ObservedAt: now,
		},
	}
	alerts := buildAlerts(snaps)
	if len(alerts) != 0 {
		t.Fatalf("expected 0 alerts for 10%% usage, got %d", len(alerts))
	}
}

func TestBuildAlerts_Warning(t *testing.T) {
	now := time.Now().UTC()
	snaps := []storage.QuotaSnapshotRecord{
		{
			ProviderID: "openai",
			AccountID:  "default",
			Dimension:  "requests",
			UsedValue:  80,
			LimitValue: ptr(100.0),
			ObservedAt: now,
		},
	}
	alerts := buildAlerts(snaps)
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert for 80%% usage, got %d", len(alerts))
	}
	a := alerts[0].(map[string]any)
	if a["severity"] != "warning" {
		t.Fatalf("expected warning severity, got %v", a["severity"])
	}
}

func TestBuildAlerts_Critical(t *testing.T) {
	now := time.Now().UTC()
	snaps := []storage.QuotaSnapshotRecord{
		{
			ProviderID: "openai",
			AccountID:  "default",
			Dimension:  "requests",
			UsedValue:  95,
			LimitValue: ptr(100.0),
			ObservedAt: now,
		},
	}
	alerts := buildAlerts(snaps)
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert for 95%% usage, got %d", len(alerts))
	}
	a := alerts[0].(map[string]any)
	if a["severity"] != "critical" {
		t.Fatalf("expected critical severity, got %v", a["severity"])
	}
}

func TestBuildQuotaRecommendations_None(t *testing.T) {
	now := time.Now().UTC()
	snaps := []storage.QuotaSnapshotRecord{
		{
			ProviderID: "openai",
			AccountID:  "default",
			Dimension:  "requests",
			UsedValue:  10,
			LimitValue: ptr(100.0),
			ObservedAt: now,
		},
	}
	recs := buildQuotaRecommendations(snaps)
	if len(recs) != 0 {
		t.Fatalf("expected 0 recommendations for 10%% usage, got %d", len(recs))
	}
}

func TestBuildQuotaRecommendations_Info(t *testing.T) {
	now := time.Now().UTC()
	snaps := []storage.QuotaSnapshotRecord{
		{
			ProviderID: "openai",
			AccountID:  "default",
			Dimension:  "requests",
			UsedValue:  55,
			LimitValue: ptr(100.0),
			ObservedAt: now,
		},
	}
	recs := buildQuotaRecommendations(snaps)
	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation for 55%% usage, got %d", len(recs))
	}
	if recs[0].Priority != "low" {
		t.Fatalf("expected low priority, got %s", recs[0].Priority)
	}
	if !strings.Contains(recs[0].Message, "multi-account routing") {
		t.Fatalf("expected multi-account routing advice, got %s", recs[0].Message)
	}
}

func TestBuildQuotaRecommendations_Warning(t *testing.T) {
	now := time.Now().UTC()
	snaps := []storage.QuotaSnapshotRecord{
		{
			ProviderID: "openai",
			AccountID:  "default",
			Dimension:  "requests",
			UsedValue:  80,
			LimitValue: ptr(100.0),
			ObservedAt: now,
		},
	}
	recs := buildQuotaRecommendations(snaps)
	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation for 80%% usage, got %d", len(recs))
	}
	if recs[0].Priority != "medium" {
		t.Fatalf("expected medium priority, got %s", recs[0].Priority)
	}
}

func TestBuildQuotaRecommendations_Critical(t *testing.T) {
	now := time.Now().UTC()
	snaps := []storage.QuotaSnapshotRecord{
		{
			ProviderID: "openai",
			AccountID:  "default",
			Dimension:  "requests",
			UsedValue:  95,
			LimitValue: ptr(100.0),
			ObservedAt: now,
		},
	}
	recs := buildQuotaRecommendations(snaps)
	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation for 95%% usage, got %d", len(recs))
	}
	if recs[0].Priority != "high" {
		t.Fatalf("expected high priority, got %s", recs[0].Priority)
	}
	if !strings.Contains(recs[0].Message, "adding another account") {
		t.Fatalf("expected account limit increase advice, got %s", recs[0].Message)
	}
}

func TestBuildQuotaRecommendations_NoLimit(t *testing.T) {
	snaps := []storage.QuotaSnapshotRecord{
		{
			ProviderID: "openai",
			AccountID:  "default",
			Dimension:  "requests",
			UsedValue:  95,
			ObservedAt: time.Now().UTC(),
		},
	}
	recs := buildQuotaRecommendations(snaps)
	if len(recs) != 0 {
		t.Fatalf("expected 0 recommendations when no limit set, got %d", len(recs))
	}
}

func TestBuildQuotaRecommendations_Deduplication(t *testing.T) {
	now := time.Now().UTC()
	snaps := []storage.QuotaSnapshotRecord{
		{
			ProviderID: "openai",
			AccountID:  "default",
			Dimension:  "requests",
			UsedValue:  95,
			LimitValue: ptr(100.0),
			ObservedAt: now,
		},
		{
			ProviderID: "openai",
			AccountID:  "default",
			Dimension:  "requests",
			UsedValue:  95,
			LimitValue: ptr(100.0),
			ObservedAt: now,
		},
	}
	recs := buildQuotaRecommendations(snaps)
	// Both records are identical key=openai/default/requests; should deduplicate.
	if len(recs) != 1 {
		t.Fatalf("expected deduplication to 1 recommendation, got %d", len(recs))
	}
}

func TestQuotaWindowDTO_LimitPresent(t *testing.T) {
	raw := `{
		"provider_id": "p1",
		"account_id": "a1",
		"dimension": "requests",
		"used": 100,
		"limit": 1000,
		"utilization": 0.1,
		"status": "healthy",
		"source": "api",
		"confidence": "high",
		"freshness_status": "fresh",
		"reset_at": null
	}`
	var dto QuotaWindowDTO
	if err := json.Unmarshal([]byte(raw), &dto); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Limit == nil {
		t.Fatal("expected limit to be non-nil")
	}
	if *dto.Limit != 1000 {
		t.Fatalf("expected limit 1000, got %v", *dto.Limit)
	}
}

func TestQuotaWindowDTO_LimitNull(t *testing.T) {
	raw := `{
		"provider_id": "p1",
		"account_id": "a1",
		"dimension": "requests",
		"used": 100,
		"limit": null,
		"utilization": 0.1,
		"status": "healthy",
		"source": "api",
		"confidence": "high",
		"freshness_status": "fresh"
	}`
	var dto QuotaWindowDTO
	if err := json.Unmarshal([]byte(raw), &dto); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Limit != nil {
		t.Fatal("expected limit to be nil")
	}
}

func TestQuotaWindowDTO_UtilizationPresent(t *testing.T) {
	raw := `{
		"provider_id": "p1",
		"account_id": "a1",
		"dimension": "tokens",
		"used": 50,
		"limit": 200,
		"utilization": 0.25,
		"status": "healthy",
		"source": "api",
		"confidence": "high",
		"freshness_status": "fresh"
	}`
	var dto QuotaWindowDTO
	if err := json.Unmarshal([]byte(raw), &dto); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Utilization == nil {
		t.Fatal("expected utilization to be non-nil")
	}
	if *dto.Utilization != 0.25 {
		t.Fatalf("expected utilization 0.25, got %v", *dto.Utilization)
	}
}

func TestQuotaWindowDTO_UtilizationNull(t *testing.T) {
	raw := `{
		"provider_id": "p1",
		"account_id": "a1",
		"dimension": "tokens",
		"used": 50,
		"limit": 200,
		"utilization": null,
		"status": "healthy",
		"source": "api",
		"confidence": "high",
		"freshness_status": "fresh"
	}`
	var dto QuotaWindowDTO
	if err := json.Unmarshal([]byte(raw), &dto); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Utilization != nil {
		t.Fatal("expected utilization to be nil")
	}
}

func TestQuotaWindowDTO_MissingOptionalFields(t *testing.T) {
	raw := `{
		"provider_id": "p1",
		"account_id": "a1",
		"dimension": "requests",
		"used": 100,
		"limit": null,
		"status": "healthy"
	}`
	var dto QuotaWindowDTO
	if err := json.Unmarshal([]byte(raw), &dto); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Utilization != nil {
		t.Fatal("expected utilization to be nil for missing field")
	}
	if dto.Source != "" {
		t.Fatalf("expected empty source for missing field, got %q", dto.Source)
	}
	if dto.FreshnessStatus != "" {
		t.Fatalf("expected empty freshness_status for missing field, got %q", dto.FreshnessStatus)
	}
}

func TestQuotaWindowDTO_MalformedResponse(t *testing.T) {
	raw := `this is not json`
	var dto QuotaWindowDTO
	err := json.Unmarshal([]byte(raw), &dto)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestPrintWindowLine_UnknownLimit(t *testing.T) {
	dto := &QuotaWindowDTO{
		ProviderID: "p1",
		AccountID:  "a1",
		Dimension:  "requests",
		Used:       100,
		Limit:      nil,
		Status:     "healthy",
	}
	var buf strings.Builder
	printWindowLine(dto, &buf)
	output := buf.String()
	if !strings.Contains(output, "unknown") {
		t.Fatalf("expected output to contain 'unknown', got %q", output)
	}
}

func TestPrintWindowLine_ZeroLimit(t *testing.T) {
	zero := 0.0
	dto := &QuotaWindowDTO{
		ProviderID: "p1",
		AccountID:  "a1",
		Dimension:  "requests",
		Used:       100,
		Limit:      &zero,
		Status:     "healthy",
	}
	var buf strings.Builder
	printWindowLine(dto, &buf)
	output := buf.String()
	if strings.Contains(output, "unknown") {
		t.Fatalf("expected no 'unknown' for zero limit, got %q", output)
	}
	if !strings.Contains(output, "0") {
		t.Fatalf("expected output to contain '0' for zero limit, got %q", output)
	}
}

func TestPrintWindowLine_UtilizationAbsent(t *testing.T) {
	dto := &QuotaWindowDTO{
		ProviderID: "p1",
		AccountID:  "a1",
		Dimension:  "requests",
		Used:       50,
		Limit:      ptr(200.0),
		Status:     "healthy",
	}
	var buf strings.Builder
	printWindowLine(dto, &buf)
	output := buf.String()
	if strings.Contains(output, "%") {
		t.Fatalf("expected no utilization percentage when utilization is nil, got %q", output)
	}
}

func TestPrintWindowLine_UtilizationZero(t *testing.T) {
	zero := 0.0
	dto := &QuotaWindowDTO{
		ProviderID:  "p1",
		AccountID:   "a1",
		Dimension:   "requests",
		Used:        0,
		Limit:       ptr(200.0),
		Utilization: &zero,
		Status:      "healthy",
	}
	var buf strings.Builder
	printWindowLine(dto, &buf)
	output := buf.String()
	if !strings.Contains(output, "(0%)") {
		t.Fatalf("expected output to contain '(0%%)' when utilization is zero, got %q", output)
	}
}

func TestPrintWindowLine_UtilizationPresent(t *testing.T) {
	util := 0.75
	dto := &QuotaWindowDTO{
		ProviderID:  "p1",
		AccountID:   "a1",
		Dimension:   "tokens",
		Used:        150,
		Limit:       ptr(200.0),
		Utilization: &util,
		Status:      "healthy",
	}
	var buf strings.Builder
	printWindowLine(dto, &buf)
	output := buf.String()
	if !strings.Contains(output, "(75%)") {
		t.Fatalf("expected output to contain '(75%%)', got %q", output)
	}
}

func TestPrintWindowLine_UnlimitedLimit(t *testing.T) {
	dto := &QuotaWindowDTO{
		ProviderID: "p1",
		AccountID:  "a1",
		Dimension:  "requests",
		Used:       100,
		Limit:      nil,
		Status:     "healthy",
	}
	var buf strings.Builder
	printWindowLine(dto, &buf)
	output := buf.String()
	// With nil limit we print "unknown" (we don't have a concept of "unlimited" in the current schema;
	// nil means unknown. The spec says distinguish unknown/null/zero, and nil limit maps to "unknown".)
	if !strings.Contains(output, "unknown") {
		t.Fatalf("expected 'unknown' for nil limit, got %q", output)
	}
}

func TestParseQuotaWindows_InvalidItem(t *testing.T) {
	raw := []any{
		map[string]any{"provider_id": "p1", "account_id": "a1", "dimension": "requests", "used": 10.0, "limit": 100.0, "status": "healthy"},
		"this is not an object",
	}
	_, err := parseQuotaWindows(raw)
	if err == nil {
		t.Fatal("expected error for invalid window item")
	}
}
