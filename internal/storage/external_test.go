package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/termrouter/termrouter/internal/smart/external"
)

// TestExternalProposalMandatoryReviewRoundTrip verifies that the
// MandatoryReview flag (§18) survives Save -> Load and that ApplyProposal
// still blocks a loaded proposal carrying the flag. This guards against the
// bug where the column was not persisted, silently clearing the flag and
// defeating the mandatory human-review safety requirement.
func TestExternalProposalMandatoryReviewRoundTrip(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	p := external.Proposal{
		ID:             "prop-1",
		ProviderID:     "openai",
		ModelID:        "gpt-5",
		ModelIdentity:  "openai/gpt-5",
		Fields:         []external.ProposalField{{Capability: "reasoning", Proposed: 8.0}},
		Status:         "pending",
		RegistryVersion: "v1",
		MandatoryReview: true,
	}
	if err := store.SaveExternalProposal(p); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, ok, err := store.LoadExternalProposal("prop-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !ok {
		t.Fatal("proposal not found after save")
	}
	if !loaded.MandatoryReview {
		t.Fatalf("MandatoryReview lost across round-trip: got false, want true")
	}

	// The external service must still refuse to apply a mandatory-review
	// proposal loaded from storage.
	svc := external.NewService(store, nil, nil)
	if _, err := svc.ApplyProposal(loaded); err == nil {
		t.Fatal("ApplyProposal should reject a loaded mandatory-review proposal")
	}

	// Sanity: a cleared proposal is applicable.
	p.MandatoryReview = false
	if err := store.SaveExternalProposal(p); err != nil {
		t.Fatalf("save cleared: %v", err)
	}
	loaded2, _, _ := store.LoadExternalProposal("prop-1")
	if loaded2.MandatoryReview {
		t.Fatal("expected cleared flag after update")
	}
	if _, err := svc.ApplyProposal(loaded2); err != nil {
		t.Fatalf("ApplyProposal should succeed for non-mandatory proposal, got: %v", err)
	}
}

// TestExternalProposalMandatoryReviewList verifies ListExternalProposals also
// restores the flag.
func TestExternalProposalMandatoryReviewList(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	p := external.Proposal{
		ID:              "prop-list-1",
		ProviderID:      "openai",
		ModelID:         "gpt-5",
		ModelIdentity:   "openai/gpt-5",
		Fields:          []external.ProposalField{{Capability: "coding", Proposed: 7.5}},
		Status:          "pending",
		RegistryVersion: "v1",
		MandatoryReview: true,
	}
	if err := store.SaveExternalProposal(p); err != nil {
		t.Fatalf("save: %v", err)
	}
	list, err := store.ListExternalProposals("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(list))
	}
	if !list[0].MandatoryReview {
		t.Fatal("ListExternalProposals dropped MandatoryReview")
	}
}

// TestExternalProposalMandatoryReviewMigration verifies that a database created
// at schema version 8 (before the mandatory_review column existed) is upgraded
// by migrate() and the flag round-trips correctly.
func TestExternalProposalMandatoryReviewMigration(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "termrouter.db")

	// Simulate a pre-v9 database: create the table without mandatory_review and
	// stamp schema_version = 8.
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	_, err = raw.Exec(`
CREATE TABLE schema_version (version INTEGER NOT NULL);
INSERT INTO schema_version(version) VALUES(8);
CREATE TABLE external_profile_proposals (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    model_id TEXT NOT NULL,
    model_identity TEXT NOT NULL,
    fields_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    status TEXT NOT NULL,
    registry_version TEXT NOT NULL
);
`)
	if err != nil {
		t.Fatalf("seed v8 schema: %v", err)
	}
	raw.Close()

	// Opening via Store triggers migrate(), which must add the column.
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store (migrate): %v", err)
	}
	defer store.Close()

	p := external.Proposal{
		ID:              "prop-mig-1",
		ProviderID:      "openai",
		ModelID:         "gpt-5",
		ModelIdentity:   "openai/gpt-5",
		Fields:          []external.ProposalField{{Capability: "coding", Proposed: 7.5}},
		Status:          "pending",
		RegistryVersion: "v1",
		MandatoryReview: true,
	}
	if err := store.SaveExternalProposal(p); err != nil {
		t.Fatalf("save after migrate: %v", err)
	}
	loaded, ok, err := store.LoadExternalProposal("prop-mig-1")
	if err != nil {
		t.Fatalf("load after migrate: %v", err)
	}
	if !ok {
		t.Fatal("proposal not found after migrate")
	}
	if !loaded.MandatoryReview {
		t.Fatal("MandatoryReview not restored after v8->v9 migration")
	}
}
