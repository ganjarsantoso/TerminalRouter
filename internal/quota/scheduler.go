package quota

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

// SchedulerConfig controls the refresh scheduler behavior.
type SchedulerConfig struct {
	// IntervalBase is the normal refresh interval.
	IntervalBase time.Duration
	// IntervalCritical is the refresh interval when quota is critical.
	IntervalCritical time.Duration
	// IntervalWarning is the refresh interval when quota is warning.
	IntervalWarning time.Duration
	// IntervalInactive is the refresh interval for inactive accounts.
	IntervalInactive time.Duration
	// JitterFraction is the max fraction of jitter (0.0-1.0).
	JitterFraction float64
}

// DefaultSchedulerConfig returns conservative defaults.
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		IntervalBase:     5 * time.Minute,
		IntervalCritical: 30 * time.Second,
		IntervalWarning:  60 * time.Second,
		IntervalInactive: 30 * time.Minute,
		JitterFraction:   0.2,
	}
}

// RefreshFunc is called to collect quota for one account.
type RefreshFunc func(ctx context.Context, providerID, accountID, providerType string) (*ProviderQuotaSnapshot, error)

// AccountProvider resolves provider type for an account.
type AccountProvider func(providerID, accountID string) (providerType string, ok bool)

// Scheduler runs periodic quota collection in the background.
type Scheduler struct {
	cfg      SchedulerConfig
	registry *Registry
	refresh  RefreshFunc
	resolve  AccountProvider
	store    SnapshotStore
	log      *slog.Logger

	mu      sync.Mutex
	tickers map[string]*time.Ticker
	cancel  context.CancelFunc
}

// SnapshotStore persists quota snapshots.
type SnapshotStore interface {
	InsertSnapshot(ctx context.Context, snap ProviderQuotaSnapshot) error
	ListAccounts() []AccountInfo
}

// AccountInfo describes an account for scheduling purposes.
type AccountInfo struct {
	ProviderID   string
	AccountID    string
	ProviderType string
	Status       QuotaStatus
}

// NewScheduler creates a background quota refresh scheduler.
func NewScheduler(cfg SchedulerConfig, registry *Registry, refresh RefreshFunc, resolve AccountProvider, store SnapshotStore, log *slog.Logger) *Scheduler {
	if cfg.JitterFraction <= 0 {
		cfg.JitterFraction = 0.2
	}
	if cfg.IntervalBase <= 0 {
		cfg = DefaultSchedulerConfig()
	}
	return &Scheduler{
		cfg:      cfg,
		registry: registry,
		refresh:  refresh,
		resolve:  resolve,
		store:    store,
		log:      log,
		tickers:  map[string]*time.Ticker{},
	}
}

// Start begins background collection for all known accounts.
func (s *Scheduler) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	accounts := s.store.ListAccounts()
	for _, acct := range accounts {
		s.scheduleAccount(ctx, acct)
	}
	s.log.Info("quota scheduler started", "accounts", len(accounts))
}

// Stop stops all background collection.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, t := range s.tickers {
		t.Stop()
		delete(s.tickers, k)
	}
	s.log.Info("quota scheduler stopped")
}

// RefreshNow triggers an immediate refresh for one account.
func (s *Scheduler) RefreshNow(ctx context.Context, providerID, accountID string) (*ProviderQuotaSnapshot, error) {
	provType, ok := s.resolve(providerID, accountID)
	if !ok {
		return nil, nil
	}
	snap, err := s.refresh(ctx, providerID, accountID, provType)
	if err != nil {
		return nil, err
	}
	if snap != nil {
		_ = s.store.InsertSnapshot(ctx, *snap)
	}
	return snap, nil
}

func (s *Scheduler) scheduleAccount(ctx context.Context, acct AccountInfo) {
	key := acct.ProviderID + "|" + acct.AccountID
	interval := s.intervalForStatus(acct.Status)
	interval = s.addJitter(interval)

	ticker := time.NewTicker(interval)
	s.mu.Lock()
	s.tickers[key] = ticker
	s.mu.Unlock()

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.collectOne(ctx, acct)
			}
		}
	}()
}

func (s *Scheduler) collectOne(ctx context.Context, acct AccountInfo) {
	snap, err := s.refresh(ctx, acct.ProviderID, acct.AccountID, acct.ProviderType)
	if err != nil {
		if s.log != nil {
			s.log.Warn("quota refresh failed",
				"provider", acct.ProviderID,
				"account", acct.AccountID,
				"error", err,
			)
		}
		return
	}
	if snap != nil {
		_ = s.store.InsertSnapshot(ctx, *snap)
	}
}

func (s *Scheduler) intervalForStatus(status QuotaStatus) time.Duration {
	switch status {
	case StatusCritical, StatusExhausted:
		return s.cfg.IntervalCritical
	case StatusWarning:
		return s.cfg.IntervalWarning
	case StatusStale:
		return s.cfg.IntervalBase
	default:
		return s.cfg.IntervalBase
	}
}

func (s *Scheduler) addJitter(d time.Duration) time.Duration {
	if s.cfg.JitterFraction <= 0 {
		return d
	}
	jitter := float64(d) * s.cfg.JitterFraction * (rand.Float64()*2 - 1)
	return d + time.Duration(jitter)
}
