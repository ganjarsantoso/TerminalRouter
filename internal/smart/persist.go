package smart

import (
	"context"
	"encoding/json"
	"time"

	"github.com/termrouter/termrouter/internal/storage"
)

// PersistDecision stores a decision without raw prompt content.
func PersistDecision(ctx context.Context, store *storage.Store, d *Decision) error {
	if store == nil || d == nil {
		return nil
	}
	reasons, _ := json.Marshal(d.SelectionReasons)
	evals, _ := json.Marshal(d.Evaluations)
	task, _ := json.Marshal(d.Task)
	return store.InsertSmartDecision(ctx, storage.SmartDecisionRecord{
		RequestID:            d.RequestID,
		RouteID:              d.RouteID,
		RequestedAlias:       d.RequestedAlias,
		Mode:                 d.Mode,
		Policy:               d.Policy,
		TaskPrimaryType:      d.Task.PrimaryType,
		TaskComplexity:       d.Task.Complexity,
		Confidence:           d.Task.Confidence,
		ClassifierVersion:    d.Task.ClassifierVersion,
		SelectedProvider:     d.SelectedProvider,
		SelectedModel:        d.SelectedModel,
		SelectionScore:       d.SelectionScore,
		SelectionReasons:     string(reasons),
		ShadowRecommendation: d.ShadowRecommendation,
		UsedDefault:          d.UsedDefault,
		DefaultReason:        d.DefaultReason,
		SessionID:            d.SessionAffinity.SessionID,
		SessionAffinityHit:   d.SessionAffinity.Hit,
		EvaluationsJSON:      string(evals),
		TaskJSON:             string(task),
		CreatedAt:            d.CreatedAt,
	})
}

// DecisionFromRecord rebuilds a Decision for explain output.
func DecisionFromRecord(r *storage.SmartDecisionRecord) *Decision {
	if r == nil {
		return nil
	}
	d := &Decision{
		RequestID:            r.RequestID,
		RouteID:              r.RouteID,
		RequestedAlias:       r.RequestedAlias,
		Mode:                 r.Mode,
		Policy:               r.Policy,
		SelectedProvider:     r.SelectedProvider,
		SelectedModel:        r.SelectedModel,
		SelectionScore:       r.SelectionScore,
		ShadowRecommendation: r.ShadowRecommendation,
		UsedDefault:          r.UsedDefault,
		DefaultReason:        r.DefaultReason,
		CatalogVersion:       CatalogVersion,
		CreatedAt:            r.CreatedAt,
		SessionAffinity: SessionAffinityResult{
			Hit:       r.SessionAffinityHit,
			SessionID: r.SessionID,
		},
		Task: TaskProfile{
			PrimaryType:       r.TaskPrimaryType,
			Complexity:        r.TaskComplexity,
			Confidence:        r.Confidence,
			ClassifierVersion: r.ClassifierVersion,
			Requirements:      map[string]int{},
		},
	}
	_ = json.Unmarshal([]byte(r.SelectionReasons), &d.SelectionReasons)
	_ = json.Unmarshal([]byte(r.EvaluationsJSON), &d.Evaluations)
	if r.TaskJSON != "" {
		var t TaskProfile
		if json.Unmarshal([]byte(r.TaskJSON), &t) == nil {
			d.Task = t
		}
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	return d
}
