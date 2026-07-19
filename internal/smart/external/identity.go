package external

import "strings"

// ModelIdentity is a model referenced for evidence lookup. It is derived
// directly from the configured provider/model — there is no curated directory;
// any model can be searched live. It carries NO scores (those are fetched live).
type ModelIdentity struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Provider string   `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
}

// identityFor builds a ModelIdentity directly from a provider/model pair.
// Any model is accepted; we only normalize the id for matching/caching.
func identityFor(providerID, modelID string) ModelIdentity {
	id := strings.TrimSpace(providerID) + "/" + strings.TrimSpace(modelID)
	return ModelIdentity{
		ID:       id,
		Name:     modelID,
		Provider: providerID,
		Model:    modelID,
	}
}
