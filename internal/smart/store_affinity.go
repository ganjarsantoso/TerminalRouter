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
	return &StoreAffinity{Store: store, Ctx: context.Background()}
}

func (a *StoreAffinity) Get(sessionID string) (AffinityRecord, bool) {
	if a.Store == nil {
		return AffinityRecord{}, false
	}
	ctx := a.Ctx
	if ctx == nil {
		ctx = context.Background()
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
		ctx = context.Background()
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
		ctx = context.Background()
	}
	return a.Store.DeleteSessionAffinity(ctx, sessionID)
}
