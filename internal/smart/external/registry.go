package external

import (
	"strings"
	"time"
)

// registryVersion is the bundled methodology/registry version. Bump when the
// source methodology or capability mapping changes (not when model scores
// change — those are fetched live).
const registryVersion = "2026.07"

// registryUpdatedAt is the (fixed) build-time reference date for the methodology bundle.
var registryUpdatedAt = time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)

// sources is the curated list of independent benchmark sources we recognize in
// search results. These define normalization methodology; the actual numbers
// are fetched live, never hardcoded.
var sources = []SourceMeta{
	{
		ID:          SourceLiveBench,
		Name:        "LiveBench",
		URL:         "https://livebench.ai",
		Domains:     []string{"livebench.ai", "github.com", "arxiv.org"},
		TrustTier:   TrustHigh,
		NativeScale: ScaleZeroToHundred,
		Description: "Contamination-free, automatically-scored LLM benchmark covering reasoning, math, coding, and language tasks. Reported as percentage (0-100).",
	},
	{
		ID:          SourceAAII,
		Name:        "Artificial Analysis Intelligence Index",
		URL:         "https://artificialanalysis.ai",
		Domains:     []string{"artificialanalysis.ai"},
		TrustTier:   TrustModerate,
		NativeScale: ScaleZeroToHundred,
		Description: "Composite intelligence index (0-100) combining multiple third-party benchmarks across agents, coding, general capability, and scientific reasoning.",
	},
	{
		ID:          SourceSWEBench,
		Name:        "SWE-bench Verified",
		URL:         "https://www.swebench.com",
		Domains:     []string{"swebench.com", "github.com"},
		TrustTier:   TrustHigh,
		NativeScale: ScaleZeroToHundred,
		Description: "Real-world software engineering resolution rate (0-100%). Maps primarily to the coding capability.",
	},
	{
		ID:          SourceLMArena,
		Name:        "LMArena",
		URL:         "https://lmarena.ai",
		Domains:     []string{"lmarena.ai", "github.com"},
		TrustTier:   TrustModerate,
		NativeScale: ScaleElo,
		Description: "Human-preference Elo arena, normalized against the arena median to a 0-10 band.",
	},
}

// sourceMetaByID returns the meta for a source id.
func sourceMetaByID(id SourceID) (SourceMeta, bool) {
	for _, s := range sources {
		if s.ID == id {
			return s, true
		}
	}
	return SourceMeta{}, false
}

// ApprovedDomains returns the flattened allowlist of hostnames from which
// automatic scoring is permitted (§15).
func ApprovedDomains() []string {
	var out []string
	for _, s := range sources {
		out = append(out, s.Domains...)
	}
	return out
}

// IsApprovedHost reports whether host (with or without port) is within an
// approved source domain or its subdomain. Exact model identities are still
// required for scoring; this only restricts the network egress/trust surface.
func IsApprovedHost(host string) bool {
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, s := range sources {
		for _, d := range s.Domains {
			d = strings.ToLower(d)
			if host == d || strings.HasSuffix(host, "."+d) {
				return true
			}
		}
	}
	return false
}

// searchQueries builds the live-search queries used to gather benchmark evidence
// for a model directly from its provider/model. Any model is supported; there
// is no curated directory. Each query targets approved independent benchmark
// sources via site: scoping (§15), instead of a generic open-web query.
func searchQueries(providerID, modelID string) []string {
	name := modelID
	model := modelID
	scoped := func(site, q string) string {
		return "site:" + site + " " + q
	}
	return []string{
		scoped("livebench.ai", name+" LiveBench overall score reasoning coding math percentage"),
		scoped("swebench.com", model+" SWE-bench Verified percentage resolved"),
		scoped("artificialanalysis.ai", name+" Artificial Analysis Intelligence Index score"),
		scoped("lmarena.ai", name+" LMArena Elo arena score"),
		scoped("arxiv.org", name+" benchmark reasoning coding math percentage"),
		scoped("github.com", name+" benchmark leaderboard methodology"),
	}
}
