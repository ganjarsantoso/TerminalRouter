package quota

import (
	"sort"
	"sync"
	"time"
)

// AccountCandidate is an account considered for selection.
type AccountCandidate struct {
	Account        ProviderAccount
	Windows        []QuotaWindowState
	InFlightResv   float64
	LastSelectedAt time.Time
	TokensToday    int64
	RequestsToday  int
	EstimatedCost  float64
}

// AccountSelector performs policy-controlled multi-account ranking.
type AccountSelector struct {
	mu    sync.Mutex
	state map[string]*AccountRoutingState // key: providerID|routeID
	// in-flight reservations by provider|account
	reserved map[string]float64
}

// NewAccountSelector creates an empty selector.
func NewAccountSelector() *AccountSelector {
	return &AccountSelector{
		state:    map[string]*AccountRoutingState{},
		reserved: map[string]float64{},
	}
}

func stateKey(providerID, routeID string) string {
	return providerID + "|" + routeID
}

func reserveKey(providerID, accountID string) string {
	return providerID + "|" + accountID
}

// Select chooses an eligible account. Quota utilization never overrides hard
// eligibility (enabled, credential, not draining, not exhausted hard limit).
// Automatic rotation requires MultiAccountRotationOK on the account.
func (s *AccountSelector) Select(
	providerID, routeID string,
	mode AccountRoutingMode,
	candidates []AccountCandidate,
	now time.Time,
	reservationEstimate float64,
) SelectionDecision {
	dec := SelectionDecision{
		ProviderID:      providerID,
		Mode:            mode,
		Rejected:        map[string]string{},
		ReservationEst:  reservationEstimate,
		ScoreComponents: map[string]float64{},
	}
	if mode == "" {
		mode = RouteFixed
		dec.Mode = mode
	}

	var eligible []AccountCandidate
	for _, c := range candidates {
		if reason := eligibilityReason(c); reason != "" {
			dec.Rejected[c.Account.ID] = reason
			continue
		}
		// Automatic rotation requires explicit policy permission when more than
		// one eligible account would be considered under a rotating mode.
		if mode != RouteFixed && mode != RouteManual && !c.Account.MultiAccountRotationOK && !c.Account.QuotaRoutingAllowed {
			dec.Rejected[c.Account.ID] = "automatic_rotation_disallowed"
			// Still allow if it's the only candidate; gate below.
			continue
		}
		eligible = append(eligible, c)
	}

	// If rotation-gated filtering removed everyone but fixed/manual would work,
	// fall back to any enabled non-drained accounts for fixed selection.
	if len(eligible) == 0 {
		for _, c := range candidates {
			if reason := eligibilityReason(c); reason == "" {
				eligible = append(eligible, c)
				delete(dec.Rejected, c.Account.ID)
			}
		}
		if mode != RouteFixed && mode != RouteManual {
			mode = RouteFixed
			dec.Mode = mode
			dec.Reason = "rotation_disallowed_fallback_fixed"
		}
	}

	if len(eligible) == 0 {
		dec.Reason = "no_eligible_accounts"
		return dec
	}

	// Sort for deterministic tie-breaking: highest score later; base order by ID.
	sort.SliceStable(eligible, func(i, j int) bool {
		return eligible[i].Account.ID < eligible[j].Account.ID
	})

	var chosen AccountCandidate
	switch mode {
	case RouteRoundRobin:
		chosen = s.roundRobin(providerID, routeID, eligible, now)
		dec.Reason = "round_robin"
	case RouteWeightedRoundRobin:
		chosen = s.weightedRoundRobin(providerID, routeID, eligible, now)
		dec.Reason = "weighted_round_robin"
	case RouteLeastUsed:
		chosen = leastUsed(eligible)
		dec.Reason = "least_used"
	case RouteMostRemaining:
		chosen = mostRemaining(eligible)
		dec.Reason = "most_remaining"
	case RouteResetAware, RouteQuotaBalanced:
		chosen = quotaBalanced(eligible, now)
		dec.Reason = string(mode)
	case RouteCostAware:
		chosen = costAware(eligible)
		dec.Reason = "cost_aware"
	case RouteManual, RouteFixed:
		fallthrough
	default:
		chosen = eligible[0]
		dec.Reason = "fixed"
	}

	for _, e := range eligible {
		dec.Eligible = append(dec.Eligible, e.Account.ID)
		if e.Account.ID != chosen.Account.ID {
			dec.FallbackOrder = append(dec.FallbackOrder, e.Account.ID)
		}
	}
	dec.SelectedAccount = chosen.Account.ID

	// Atomic-ish reservation against selected account.
	s.mu.Lock()
	s.reserved[reserveKey(providerID, chosen.Account.ID)] += reservationEstimate
	sk := stateKey(providerID, routeID)
	st := s.state[sk]
	if st == nil {
		st = &AccountRoutingState{ProviderID: providerID, RouteID: routeID}
		s.state[sk] = st
	}
	st.LastAccountID = chosen.Account.ID
	st.Sequence++
	st.UpdatedAt = now.UTC()
	s.mu.Unlock()

	return dec
}

// ReleaseReservation subtracts an unused reservation after request finalization.
func (s *AccountSelector) ReleaseReservation(providerID, accountID string, amount float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := reserveKey(providerID, accountID)
	s.reserved[k] -= amount
	if s.reserved[k] <= 0 {
		delete(s.reserved, k)
	}
}

// Reservation returns current in-flight reservation for an account.
func (s *AccountSelector) Reservation(providerID, accountID string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reserved[reserveKey(providerID, accountID)]
}

func eligibilityReason(c AccountCandidate) string {
	if !c.Account.Enabled {
		return "disabled"
	}
	if c.Account.Draining {
		return "draining"
	}
	if !c.Account.CredentialAvailable {
		return "credential_unavailable"
	}
	// Hard-exhausted windows with hard_limit enforcement exclude the account.
	for _, w := range c.Windows {
		if w.Status == StatusExhausted {
			return "quota_exhausted"
		}
		if w.Remaining != nil && *w.Remaining <= 0 && w.Limit != nil {
			return "quota_exhausted"
		}
	}
	return ""
}

func (s *AccountSelector) roundRobin(providerID, routeID string, eligible []AccountCandidate, now time.Time) AccountCandidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	sk := stateKey(providerID, routeID)
	st := s.state[sk]
	start := 0
	if st != nil && st.LastAccountID != "" {
		for i, e := range eligible {
			if e.Account.ID == st.LastAccountID {
				start = (i + 1) % len(eligible)
				break
			}
		}
	}
	return eligible[start]
}

func (s *AccountSelector) weightedRoundRobin(providerID, routeID string, eligible []AccountCandidate, now time.Time) AccountCandidate {
	// Expand by weight then round-robin.
	var expanded []AccountCandidate
	for _, e := range eligible {
		w := e.Account.RoutingWeight
		if w <= 0 {
			w = 1
		}
		for i := 0; i < w; i++ {
			expanded = append(expanded, e)
		}
	}
	return s.roundRobin(providerID, routeID, expanded, now)
}

func leastUsed(eligible []AccountCandidate) AccountCandidate {
	best := eligible[0]
	for _, e := range eligible[1:] {
		if e.TokensToday < best.TokensToday {
			best = e
			continue
		}
		if e.TokensToday == best.TokensToday && e.RequestsToday < best.RequestsToday {
			best = e
		}
	}
	return best
}

func mostRemaining(eligible []AccountCandidate) AccountCandidate {
	best := eligible[0]
	bestRem := maxRemaining(best)
	for _, e := range eligible[1:] {
		r := maxRemaining(e)
		if r > bestRem {
			best = e
			bestRem = r
		}
	}
	return best
}

func maxRemaining(c AccountCandidate) float64 {
	var best float64 = -1
	for _, w := range c.Windows {
		if w.Remaining != nil && *w.Remaining > best {
			best = *w.Remaining
		}
	}
	if best < 0 {
		// Unknown remaining: treat as moderately available but less than known high remaining.
		return 0
	}
	return best
}

func quotaBalanced(eligible []AccountCandidate, now time.Time) AccountCandidate {
	// Score: remaining util inverted + reset opportunity - exhaustion risk - stale penalty.
	best := eligible[0]
	bestScore := scoreAccount(best, now)
	for _, e := range eligible[1:] {
		sc := scoreAccount(e, now)
		if sc > bestScore {
			best = e
			bestScore = sc
		}
	}
	return best
}

func scoreAccount(c AccountCandidate, now time.Time) float64 {
	score := 0.0
	// Prefer lower utilization.
	var utilSum float64
	var utilN int
	for _, w := range c.Windows {
		if w.Utilization != nil {
			utilSum += *w.Utilization
			utilN++
		}
		if w.Status == StatusStale {
			score -= 0.2
		}
		if w.ForecastStatus == ForecastLikelyExhaust {
			score -= 0.5
		}
		if w.ResetAt != nil && !w.ResetAt.IsZero() {
			// Prefer accounts with more time until reset when underutilized.
			hrs := w.ResetAt.Sub(now).Hours()
			if hrs > 0 && w.Utilization != nil && *w.Utilization < 0.5 {
				score += 0.1
			}
		}
	}
	if utilN > 0 {
		avg := utilSum / float64(utilN)
		score += (1 - avg) // remaining quota score
	} else {
		score += 0.5 // unknown: neutral
	}
	// Prefer lower in-flight reservation.
	score -= c.InFlightResv * 0.01
	// Older last-selected wins slightly (fairness).
	if !c.LastSelectedAt.IsZero() {
		score += now.Sub(c.LastSelectedAt).Hours() * 0.001
	}
	return score
}

func costAware(eligible []AccountCandidate) AccountCandidate {
	best := eligible[0]
	for _, e := range eligible[1:] {
		if e.EstimatedCost < best.EstimatedCost {
			best = e
		}
	}
	return best
}
