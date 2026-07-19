package external

import "strings"

// aliasIndex maps lowercased alias strings to a canonical ModelIdentity.
var aliasIndex map[string]ModelIdentity

func init() {
	aliasIndex = make(map[string]ModelIdentity, len(identities)*4)
	for _, id := range identities {
		aliasIndex[strings.ToLower(id.ID)] = id
		aliasIndex[strings.ToLower(id.Name)] = id
		if id.Provider != "" && id.Model != "" {
			aliasIndex[strings.ToLower(id.Provider+"/"+id.Model)] = id
		}
		for _, a := range id.Aliases {
			aliasIndex[strings.ToLower(a)] = id
		}
	}
}

// ResolveIdentity maps a provider/model pair (or any known alias) to a canonical
// ModelIdentity. Returns (identity, true) when resolved.
func ResolveIdentity(providerID, modelID string) (ModelIdentity, bool) {
	// Exact provider/model.
	if providerID != "" && modelID != "" {
		if id, ok := aliasIndex[strings.ToLower(providerID+"/"+modelID)]; ok {
			return id, true
		}
	}
	// Bare model id.
	if modelID != "" {
		if id, ok := aliasIndex[strings.ToLower(modelID)]; ok {
			return id, true
		}
	}
	// Any alias containing the model id as a token.
	if modelID != "" {
		lm := strings.ToLower(modelID)
		for alias, id := range aliasIndex {
			if alias == lm || strings.Contains(alias, lm) {
				return id, true
			}
		}
	}
	return ModelIdentity{}, false
}

// ListIdentities returns all curated model identities.
func ListIdentities() []ModelIdentity {
	out := make([]ModelIdentity, len(identities))
	copy(out, identities)
	return out
}
