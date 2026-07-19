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
			// base model (strip date/version suffixes like -2024-08-06)
			base := stripVersionSuffix(id.Model)
			if base != id.Model {
				aliasIndex[strings.ToLower(id.Provider+"/"+base)] = id
			}
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
		// Version/date-suffix-stripped bare model id (e.g. gpt-4o-2024-08-06).
		if id, ok := aliasIndex[strings.ToLower(stripVersionSuffix(modelID))]; ok {
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

// stripVersionSuffix removes a trailing -YYYY-MM-DD version suffix so dated
// model ids map to their family baseline.
func stripVersionSuffix(model string) string {
	for i := 0; i+10 <= len(model); i++ {
		seg := model[i : i+10]
		if seg[4] == '-' && seg[7] == '-' && isDigits(seg[:4]) && isDigits(seg[5:7]) && isDigits(seg[8:]) {
			if i > 0 && model[i-1] == '-' {
				return model[:i-1]
			}
		}
	}
	return model
}

func isDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// ListIdentities returns all curated model identities.
func ListIdentities() []ModelIdentity {
	out := make([]ModelIdentity, len(identities))
	copy(out, identities)
	return out
}
