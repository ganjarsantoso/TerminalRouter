package smart

import (
	"context"
	"time"

	"github.com/termrouter/termrouter/internal/storage"
)

// StoreAffinity adapts storage.Store to AffinityStore.
type StoreAffinity struct {
	Store *storage.Store
	Ctx   context.Context
}

func NewStoreAffinity(store *storage.Store) *StoreAffinity {
	// Callers of NewStoreAffinity should set Ctx to a request-scoped context
	// or lifecycle context before using Get/Put/Delete. The zero default is a
	// bounded fallback that prevents indefinite goroutine hangs.
	bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cancel() // cancelled immediately; caller must replace Ctx before use
	return &StoreAffinity{Store: store, Ctx: bgCtx}
}

func (a *StoreAffinity) Get(sessionID string) (AffinityRecord, bool) {
	if a.Store == nil {
		return AffinityRecord{}, false
	}
	ctx := a.Ctx
	if ctx == nil {
		bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ctx = bgCtx
	}
	r, err := a.Store.GetSessionAffinity(ctx, sessionID)
	if err != nil || r == nil {
		return AffinityRecord{}, false
	}
	return AffinityRecord{
		SessionID:  r.SessionID,
		RouteID:    r.RouteID,
		Provider:   r.Provider,
		Model:      r.Model,
		ExpiresAt:  r.ExpiresAt,
		TaskType:   r.TaskType,
		Complexity: r.Complexity,
	}, true
}

func (a *StoreAffinity) Put(rec AffinityRecord) error {
	if a.Store == nil {
		return nil
	}
	ctx := a.Ctx
	if ctx == nil {
		bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ctx = bgCtx
	}
	return a.Store.UpsertSessionAffinity(ctx, storage.SessionAffinityRecord{
		SessionID:  rec.SessionID,
		RouteID:    rec.RouteID,
		Provider:   rec.Provider,
		Model:      rec.Model,
		TaskType:   rec.TaskType,
		Complexity: rec.Complexity,
		ExpiresAt:  rec.ExpiresAt,
		UpdatedAt:  time.Now().UTC(),
	})
}

func (a *StoreAffinity) Delete(sessionID string) error {
	if a.Store == nil {
		return nil
	}
	ctx := a.Ctx
	if ctx == nil {
		bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ctx = bgCtx
	}
	return a.Store.DeleteSessionAffinity(ctx, sessionID)
}
