package external

import (
	"net"
	"strings"
	"testing"
)

func TestIsRestrictedIP(t *testing.T) {
	restricted := []string{
		"127.0.0.1",
		"10.0.0.5",
		"192.168.1.1",
		"169.254.169.254", // cloud metadata
		"169.254.1.1",
		"::1",
		"fe80::1",
		"0.0.0.0",
	}
	for _, s := range restricted {
		if !isRestrictedIP(net.ParseIP(s)) {
			t.Fatalf("expected %s to be restricted", s)
		}
	}
	allowed := []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"}
	for _, s := range allowed {
		if isRestrictedIP(net.ParseIP(s)) {
			t.Fatalf("expected %s to be allowed", s)
		}
	}
}

func TestIsApprovedHost(t *testing.T) {
	approved := []string{
		"livebench.ai", "www.livebench.ai", "github.com", "arxiv.org",
		"artificialanalysis.ai", "swebench.com", "lmarena.ai",
	}
	for _, h := range approved {
		if !IsApprovedHost(h) {
			t.Fatalf("expected %s approved", h)
		}
	}
	notApproved := []string{"evil.com", "fake.livebench.evil.com", "localhost", "169.254.169.254"}
	for _, h := range notApproved {
		if IsApprovedHost(h) {
			t.Fatalf("expected %s NOT approved", h)
		}
	}
	// port suffix handling
	if !IsApprovedHost("livebench.ai:443") {
		t.Fatal("approved host with port should pass")
	}
}

func TestIsApprovedURL(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"", true},
		{"https://livebench.ai/leaderboard", true},
		{"https://github.com/LiveBench/LiveBench", true},
		{"https://s.jina.ai/https://livebench.ai/x", true},
		{"https://evil.example.com/benchmark", false},
		{"https://random.blogspot.com/post", false},
	}
	for _, c := range cases {
		if got := IsApprovedURL(c.raw); got != c.want {
			t.Fatalf("IsApprovedURL(%q)=%v want %v", c.raw, got, c.want)
		}
	}
}

func TestValidateFetchURL(t *testing.T) {
	// Unsupported scheme rejected without network.
	if _, err := ValidateFetchURL("ftp://example.com/x"); err == nil {
		t.Fatal("expected scheme rejection")
	}
	// Unresolvable host is rejected (safe default).
	if _, err := ValidateFetchURL("https://no-such-host.invalid/x"); err == nil {
		t.Fatal("expected resolution failure rejection")
	}
}

func TestValidateEvidenceRecord(t *testing.T) {
	// unknown source
	if vr := ValidateEvidenceRecord(EvidenceRecord{Source: "bogus", Benchmark: "b", Value: 50, Scale: ScaleZeroToHundred, URL: "https://livebench.ai/x"}); vr.OK {
		t.Fatal("unknown source should fail")
	}
	// missing benchmark
	if vr := ValidateEvidenceRecord(EvidenceRecord{Source: SourceLiveBench, Benchmark: "", Value: 50, Scale: ScaleZeroToHundred, URL: "https://livebench.ai/x"}); vr.OK {
		t.Fatal("missing benchmark should fail")
	}
	// missing value
	if vr := ValidateEvidenceRecord(EvidenceRecord{Source: SourceLiveBench, Benchmark: "b", Value: 0, Scale: ScaleZeroToHundred, URL: "https://livebench.ai/x"}); vr.OK {
		t.Fatal("zero value should fail")
	}
	// out-of-range elo
	if vr := ValidateEvidenceRecord(EvidenceRecord{Source: SourceLMArena, Benchmark: "b", Value: 5000, Scale: ScaleElo, URL: "https://lmarena.ai/x"}); vr.OK {
		t.Fatal("out-of-range elo should fail")
	}
	// valid + approved
	if vr := ValidateEvidenceRecord(EvidenceRecord{Source: SourceLiveBench, Benchmark: "livebench/overall", Value: 72.5, Scale: ScaleZeroToHundred, URL: "https://livebench.ai/leaderboard"}); !vr.OK || vr.Unverified {
		t.Fatalf("valid approved record should pass: %+v", vr)
	}
	// valid but unverified (non-approved url) -> OK but Unverified
	if vr := ValidateEvidenceRecord(EvidenceRecord{Source: SourceLiveBench, Benchmark: "livebench/overall", Value: 72.5, Scale: ScaleZeroToHundred, URL: "https://someblog.example.com/post"}); !vr.OK || !vr.Unverified {
		t.Fatalf("valid unverified record should be OK+Unverified: %+v", vr)
	}
}

func TestSearchQueriesScoped(t *testing.T) {
	qs := searchQueries("openai", "gpt-4o")
	for _, q := range qs {
		if !strings.Contains(q, "site:") {
			t.Fatalf("query not scoped to approved domain: %q", q)
		}
	}
}

func TestConsensusExcludesUnverifiedEvidence(t *testing.T) {
	id := identityFor("openai", "gpt-4o")
	// All records from non-approved domains must contribute zero weight.
	recs := []EvidenceRecord{
		{Source: SourceLiveBench, ModelIdentity: id.ID, Benchmark: "livebench/overall", Value: 72, Scale: ScaleZeroToHundred, Capability: CapGeneral, URL: "https://someblog.example.com/post"},
		{Source: SourceSWEBench, ModelIdentity: id.ID, Benchmark: "swebench/verified", Value: 51, Scale: ScaleZeroToHundred, Capability: CapCoding, URL: "https://forum.example.org/x"},
	}
	cp := buildConsensus(id, recs)
	if len(cp.Capabilities) != 0 {
		t.Fatalf("unverified evidence must not contribute: got %d capabilities", len(cp.Capabilities))
	}
	// Approved-domain records must contribute.
	recs[0].URL = "https://livebench.ai/leaderboard"
	recs[1].URL = "https://www.swebench.com/verified"
	cp = buildConsensus(id, recs)
	if len(cp.Capabilities) == 0 {
		t.Fatal("approved evidence should contribute")
	}
}
