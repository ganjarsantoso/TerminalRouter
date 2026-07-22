package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/termrouter/termrouter/internal/app"
	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/storage"
)

func newQuotaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quota",
		Short: "Quota tracking and usage analytics",
	}
	cmd.AddCommand(
		newQuotaStatusCmd(),
		newQuotaWindowsCmd(),
		newQuotaReportCmd(),
		newQuotaRefreshCmd(),
		newQuotaEventsCmd(),
		newQuotaRecommendationsCmd(),
	)
	return cmd
}

func loadQuotaStore() (*config.Config, *storage.Store, error) {
	home, err := homeDir()
	if err != nil {
		return nil, nil, err
	}
	cfg, paths, store, _, err := app.LoadRuntime(home)
	if err != nil {
		return nil, nil, err
	}
	_ = paths
	return cfg, store, nil
}

func newQuotaStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current quota status for all accounts",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, store, err := loadQuotaStore()
			if err != nil {
				return err
			}
			defer store.Close()

			snaps, err := store.LatestQuotaSnapshots(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to fetch quota snapshots: %w", err)
			}

			result := buildQuotaSummaryMap(cmd.Context(), store, cfg, snaps)
			if flagJSON {
				return printJSON(result)
			}
			printQuotaSummary(result)
			return nil
		},
	}
}

func newQuotaWindowsCmd() *cobra.Command {
	var provider, account string
	cmd := &cobra.Command{
		Use:   "windows",
		Short: "List quota windows",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, err := loadQuotaStore()
			if err != nil {
				return err
			}
			defer store.Close()

			var snaps []storage.QuotaSnapshotRecord
			if provider != "" && account != "" {
				snaps, err = store.LatestQuotaSnapshotsForAccount(cmd.Context(), provider, account)
			} else {
				snaps, err = store.LatestQuotaSnapshots(cmd.Context())
			}
			if err != nil {
				return fmt.Errorf("failed to fetch quota windows: %w", err)
			}

			result := map[string]any{
				"windows": snapshotRecordsToAny(snaps),
			}
			if flagJSON {
				return printJSON(result)
			}
			printQuotaWindows(result)
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "Filter by provider ID")
	cmd.Flags().StringVar(&account, "account", "", "Filter by account ID")
	return cmd
}

func newQuotaReportCmd() *cobra.Command {
	var last string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Show usage report",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, err := loadQuotaStore()
			if err != nil {
				return err
			}
			defer store.Close()

			dur := 7 * 24 * time.Hour
			if last != "" {
				d, err := ParseLookback(last)
				if err != nil {
					return fmt.Errorf("invalid --last: %w", err)
				}
				dur = d
			}
			since := time.Now().UTC().Add(-dur)
			sum, err := store.UsageSince(cmd.Context(), since)
			if err != nil {
				return fmt.Errorf("failed to fetch usage report: %w", err)
			}

			result := map[string]any{
				"since": since.Format(time.RFC3339),
				"summary": map[string]any{
					"total_requests": float64(sum.TotalRequests),
					"input_tokens":   float64(sum.InputTokens),
					"output_tokens":  float64(sum.OutputTokens),
					"estimated_usd":  0.0,
				},
			}
			if flagJSON {
				return printJSON(result)
			}
			printUsageReport(result)
			return nil
		},
	}
	cmd.Flags().StringVar(&last, "last", "7d", "Lookback window (e.g. 7d, 24h)")
	return cmd
}

func newQuotaRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Force a local quota re-aggregation from request log",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, err := loadQuotaStore()
			if err != nil {
				return err
			}
			defer store.Close()

			now := time.Now().UTC()
			// Aggregate today's usage from request_log and store as snapshots.
			if err := aggregateTodayUsage(cmd.Context(), store, now); err != nil {
				return fmt.Errorf("failed to refresh quota: %w", err)
			}

			ts := now.Format(time.RFC3339)
			result := map[string]any{
				"timestamp": ts,
			}
			if flagJSON {
				return printJSON(result)
			}
			fmt.Printf("Quota refreshed at %s\n", ts)
			return nil
		},
	}
}

func newQuotaEventsCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Show recent quota events",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, err := loadQuotaStore()
			if err != nil {
				return err
			}
			defer store.Close()

			events, err := store.ListQuotaEvents(cmd.Context(), limit)
			if err != nil {
				return fmt.Errorf("failed to fetch quota events: %w", err)
			}

			raw := make([]any, 0, len(events))
			for _, e := range events {
				raw = append(raw, map[string]any{
					"event_type": e.EventType,
					"message":    e.Message,
					"created_at": e.CreatedAt.Format(time.RFC3339),
				})
			}
			result := map[string]any{"events": raw}
			if flagJSON {
				return printJSON(result)
			}
			printQuotaEvents(result)
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Max events to show")
	return cmd
}

func newQuotaRecommendationsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recommendations",
		Short: "Show quota-based recommendations for account routing",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, err := loadQuotaStore()
			if err != nil {
				return err
			}
			defer store.Close()

			snaps, err := store.LatestQuotaSnapshots(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to fetch quota snapshots: %w", err)
			}

			recs := buildQuotaRecommendations(snaps)
			if flagJSON {
				return printJSON(recs)
			}
			if len(recs) == 0 {
				fmt.Println("No quota recommendations at this time.")
				return nil
			}
			fmt.Println("QUOTA RECOMMENDATIONS")
			fmt.Println("====================")
			for _, r := range recs {
				fmt.Printf("  %s/%s: %s\n", r.ProviderID, r.AccountID, r.Message)
			}
			return nil
		},
	}
}

// --- Aggregation helpers ---

func aggregateTodayUsage(ctx context.Context, store *storage.Store, now time.Time) error {
	since := now.Truncate(24 * time.Hour)
	sum, err := store.UsageSince(ctx, since)
	if err != nil {
		return err
	}

	// Store aggregate as snapshots per provider.
	for provider, count := range sum.ByProvider {
		used := float64(count)
		snap := storage.QuotaSnapshotRecord{
			ProviderID: provider,
			AccountID:  "default",
			Dimension:  "requests",
			UsedValue:  used,
			Source:     "local_authoritative",
			Confidence: 1.0,
			ObservedAt: now,
		}
		if err := store.InsertQuotaSnapshot(ctx, snap); err != nil {
			return err
		}
	}

	// Store a token snapshot.
	snap := storage.QuotaSnapshotRecord{
		ProviderID: "_total",
		AccountID:  "default",
		Dimension:  "total_tokens",
		UsedValue:  float64(sum.InputTokens + sum.OutputTokens),
		Source:     "local_authoritative",
		Confidence: 1.0,
		ObservedAt: now,
	}
	if err := store.InsertQuotaSnapshot(ctx, snap); err != nil {
		return err
	}

	return nil
}

func buildQuotaSummaryMap(ctx context.Context, store *storage.Store, cfg *config.Config, snaps []storage.QuotaSnapshotRecord) map[string]any {
	now := time.Now().UTC()
	since := now.Truncate(24 * time.Hour)

	accounts := map[string]bool{}
	var requestsToday float64
	var tokensToday float64
	for _, s := range snaps {
		accounts[s.ProviderID+"/"+s.AccountID] = true
		if s.Dimension == "requests" {
			requestsToday += s.UsedValue
		}
		if s.Dimension == "total_tokens" || s.Dimension == "input_tokens" || s.Dimension == "output_tokens" {
			tokensToday += s.UsedValue
		}
	}

	// Try to get actual usage from request_log.
	if sum, err := store.UsageSince(ctx, since); err == nil {
		requestsToday = float64(sum.TotalRequests)
		tokensToday = float64(sum.InputTokens + sum.OutputTokens)
	}

	windows := snapshotRecordsToAny(snaps)

	freshness := map[string]any{
		"status": "fresh",
	}
	if len(snaps) == 0 {
		freshness["status"] = "unknown"
	}

	alerts := buildAlerts(snaps)

	return map[string]any{
		"active_accounts":      float64(len(accounts)),
		"requests_today":       requestsToday,
		"tokens_today":         tokensToday,
		"estimated_cost_month": 0.0,
		"freshness":            freshness,
		"windows":              windows,
		"alerts":               alerts,
	}
}

func snapshotRecordsToAny(snaps []storage.QuotaSnapshotRecord) []any {
	out := make([]any, 0, len(snaps))
	for _, s := range snaps {
		var lim *float64
		if s.LimitValue != nil {
			lim = s.LimitValue
		}
		var util *float64
		if lim != nil && *lim > 0 {
			u := s.UsedValue / *lim
			util = &u
		}
		status := "healthy"
		if util != nil && *util >= 0.9 {
			status = "critical"
		} else if util != nil && *util >= 0.75 {
			status = "warning"
		}

		m := map[string]any{
			"provider_id":      s.ProviderID,
			"account_id":       s.AccountID,
			"dimension":        s.Dimension,
			"used":             s.UsedValue,
			"limit":            nil,
			"utilization":      nil,
			"status":           status,
			"source":           s.Source,
			"confidence":       fmt.Sprintf("%.1f", s.Confidence),
			"freshness_status": "fresh",
		}
		if lim != nil {
			m["limit"] = *lim
		}
		if util != nil {
			m["utilization"] = *util
		}
		if s.ObservedAt.Add(5 * time.Minute).Before(time.Now()) {
			m["freshness_status"] = "stale"
		}
		out = append(out, m)
	}
	return out
}

func buildAlerts(snaps []storage.QuotaSnapshotRecord) []any {
	var alerts []any
	for _, s := range snaps {
		var lim *float64
		if s.LimitValue != nil {
			lim = s.LimitValue
		}
		if lim != nil && *lim > 0 {
			util := s.UsedValue / *lim
			if util >= 0.9 {
				alerts = append(alerts, map[string]any{
					"severity": "critical",
					"message":  fmt.Sprintf("%s/%s %s at %.0f%% of limit", s.ProviderID, s.AccountID, s.Dimension, util*100),
				})
			} else if util >= 0.75 {
				alerts = append(alerts, map[string]any{
					"severity": "warning",
					"message":  fmt.Sprintf("%s/%s %s at %.0f%% of limit", s.ProviderID, s.AccountID, s.Dimension, util*100),
				})
			}
		}
	}
	return alerts
}

type QuotaRecommendation struct {
	ProviderID string `json:"provider_id"`
	AccountID  string `json:"account_id"`
	Dimension  string `json:"dimension"`
	Message    string `json:"message"`
	Priority   string `json:"priority"`
}

func buildQuotaRecommendations(snaps []storage.QuotaSnapshotRecord) []QuotaRecommendation {
	var recs []QuotaRecommendation
	seen := map[string]bool{}

	for _, s := range snaps {
		key := s.ProviderID + "/" + s.AccountID + "/" + s.Dimension
		if seen[key] {
			continue
		}
		seen[key] = true

		var lim *float64
		if s.LimitValue != nil {
			lim = s.LimitValue
		}
		if lim == nil || *lim <= 0 {
			continue
		}
		util := s.UsedValue / *lim

		switch {
		case util >= 0.9:
			recs = append(recs, QuotaRecommendation{
				ProviderID: s.ProviderID,
				AccountID:  s.AccountID,
				Dimension:  s.Dimension,
				Message:    fmt.Sprintf("CRITICAL: %s usage at %.0f%% — consider adding another account or requesting a limit increase", s.Dimension, util*100),
				Priority:   "high",
			})
		case util >= 0.75:
			recs = append(recs, QuotaRecommendation{
				ProviderID: s.ProviderID,
				AccountID:  s.AccountID,
				Dimension:  s.Dimension,
				Message:    fmt.Sprintf("WARNING: %s usage at %.0f%% — monitor closely and plan for additional capacity", s.Dimension, util*100),
				Priority:   "medium",
			})
		case util >= 0.5:
			recs = append(recs, QuotaRecommendation{
				ProviderID: s.ProviderID,
				AccountID:  s.AccountID,
				Dimension:  s.Dimension,
				Message:    fmt.Sprintf("INFO: %s usage at %.0f%% — consider multi-account routing to distribute load", s.Dimension, util*100),
				Priority:   "low",
			})
		}
	}
	return recs
}

// --- Printer functions ---

func printQuotaSummary(result map[string]any) {
	fmt.Println("QUOTA SUMMARY")
	fmt.Println("=============")

	if v, ok := result["active_accounts"].(float64); ok {
		fmt.Printf("Active accounts:     %.0f\n", v)
	}
	if v, ok := result["requests_today"].(float64); ok {
		fmt.Printf("Requests today:      %.0f\n", v)
	}
	if v, ok := result["tokens_today"].(float64); ok {
		fmt.Printf("Tokens today:        %.0f\n", v)
	}
	if v, ok := result["estimated_cost_month"].(float64); ok {
		fmt.Printf("Est. cost (month):   $%.2f\n", v)
	}

	if fresh, ok := result["freshness"].(map[string]any); ok {
		if status, ok := fresh["status"].(string); ok {
			fmt.Printf("Data freshness:      %s\n", status)
		}
	}

	// Print windows.
	if raw, ok := result["windows"].([]any); ok && len(raw) > 0 {
		windows, err := parseQuotaWindows(raw)
		if err == nil {
			fmt.Println()
			fmt.Println("QUOTA WINDOWS")
			for _, w := range windows {
				printWindowLine(w, os.Stdout)
			}
		}
	}

	// Print alerts.
	if alerts, ok := result["alerts"].([]any); ok && len(alerts) > 0 {
		fmt.Println()
		fmt.Println("ALERTS")
		for _, a := range alerts {
			if am, ok := a.(map[string]any); ok {
				severity, _ := am["severity"].(string)
				message, _ := am["message"].(string)
				fmt.Printf("  [%s] %s\n", severity, message)
			}
		}
	}
}

func parseQuotaWindows(raw []any) ([]*QuotaWindowDTO, error) {
	out := make([]*QuotaWindowDTO, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unexpected window item type %T", item)
		}
		b, err := json.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("marshal window item: %w", err)
		}
		var dto QuotaWindowDTO
		if err := json.Unmarshal(b, &dto); err != nil {
			return nil, fmt.Errorf("unmarshal window item: %w", err)
		}
		out = append(out, &dto)
	}
	return out, nil
}

func printQuotaWindows(result map[string]any) {
	raw, ok := result["windows"].([]any)
	if !ok || len(raw) == 0 {
		fmt.Println("No quota windows found.")
		return
	}
	windows, err := parseQuotaWindows(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing windows: %v\n", err)
		return
	}
	fmt.Println("QUOTA WINDOWS")
	fmt.Println("=============")
	for _, w := range windows {
		printWindowLine(w, os.Stdout)
	}
}

func printWindowLine(dto *QuotaWindowDTO, w io.Writer) {
	limitStr := "unknown"
	if dto.Limit != nil {
		limitStr = fmt.Sprintf("%.0f", *dto.Limit)
	}
	utilStr := ""
	if dto.Utilization != nil {
		utilStr = fmt.Sprintf(" (%.0f%%)", *dto.Utilization*100)
	}

	fmt.Fprintf(w, "  %s/%s  %s  used: %.0f / %s%s  [%s]\n",
		dto.ProviderID, dto.AccountID, dto.Dimension, dto.Used, limitStr, utilStr, dto.Status)
}

func printUsageReport(result map[string]any) {
	fmt.Println("USAGE REPORT")
	fmt.Println("============")

	if since, ok := result["since"].(string); ok {
		fmt.Printf("Since: %s\n", since)
	}

	if summary, ok := result["summary"].(map[string]any); ok {
		if v, ok := summary["total_requests"].(float64); ok {
			fmt.Printf("Total requests:  %.0f\n", v)
		}
		if v, ok := summary["input_tokens"].(float64); ok {
			fmt.Printf("Input tokens:    %.0f\n", v)
		}
		if v, ok := summary["output_tokens"].(float64); ok {
			fmt.Printf("Output tokens:   %.0f\n", v)
		}
		if v, ok := summary["estimated_usd"].(float64); ok {
			fmt.Printf("Estimated cost:  $%.4f\n", v)
		}
	}
}

func printQuotaEvents(result map[string]any) {
	events, ok := result["events"].([]any)
	if !ok || len(events) == 0 {
		fmt.Println("No recent quota events.")
		return
	}
	fmt.Println("QUOTA EVENTS")
	fmt.Println("============")
	for _, e := range events {
		if em, ok := e.(map[string]any); ok {
			eventType, _ := em["event_type"].(string)
			message, _ := em["message"].(string)
			createdAt, _ := em["created_at"].(string)
			fmt.Printf("  [%s] %s  %s\n", eventType, createdAt, message)
		}
	}
}
