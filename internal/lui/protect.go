package lui

// DefaultProtection returns the protection class for a constraint type when the
// client did not specify one. Security and compliance constraints default to
// immutable; explicit user constraints default to protected.
func DefaultProtection(constraintType string) ProtectionClass {
	switch constraintType {
	case "no_commit", "no_push", "security", "compliance", "authorization",
		"privacy", "legal", "route_policy", "key_policy":
		return ProtectionImmutable
	default:
		return ProtectionOptional
	}
}

// ApplyDefaults fills missing protection/source fields on constraints using
// safe defaults (idempotent).
func ApplyDefaults(cs []Constraint) []Constraint {
	out := make([]Constraint, 0, len(cs))
	for _, c := range cs {
		if c.Protection == "" {
			c.Protection = DefaultProtection(c.Type)
		}
		if c.Source == "" {
			c.Source = SourceClientExplicit
		}
		if c.ID == "" {
			c.ID = c.Type + ":" + c.Value
		}
		out = append(out, c)
	}
	return out
}

// MergeConstraints adds incoming constraints, refusing any override that would
// let a lower-authority source displace a higher-authority one of the same ID.
// Existing entries win on conflict unless the incoming source outranks them.
func MergeConstraints(existing []Constraint, incoming ...Constraint) []Constraint {
	byID := map[string]int{}
	for i, c := range existing {
		byID[c.ID] = i
	}
	out := append([]Constraint{}, existing...)
	for _, inc := range incoming {
		if inc.ID == "" {
			inc.ID = inc.Type + ":" + inc.Value
		}
		if idx, ok := byID[inc.ID]; ok {
			if !CanOverride(inc.Source, out[idx].Source) {
				// Lower authority cannot override; keep existing.
				continue
			}
			// Higher authority overrides existing.
			out[idx] = inc
			continue
		}
		out = append(out, inc)
		byID[inc.ID] = len(out) - 1
	}
	return out
}
