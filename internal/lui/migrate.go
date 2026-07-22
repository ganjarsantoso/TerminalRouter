package lui

import (
	"fmt"
)

// Migrate converts an envelope to the target version. The workflow is:
//  1. Validate source envelope under its current version
//  2. Deep copy
//  3. Run defined migration steps (none defined yet for v0.1)
//  4. Recompute integrity hash
//  5. Validate target envelope
//
// Same-version copy is always acceptable. Cross-version requests without
// defined migration rules return an unsupported-migration error.
func Migrate(env *Envelope, targetVersion string) (*Envelope, error) {
	if env == nil {
		return nil, fmt.Errorf("lui: cannot migrate nil envelope")
	}
	if targetVersion == "" {
		targetVersion = Version
	}

	// 1. Validate source envelope under its current version.
	if err := Validate(env); err != nil {
		return nil, fmt.Errorf("lui: source envelope invalid: %w", err)
	}

	sourceMajor := majorOf(env.Version)
	targetMajor := majorOf(targetVersion)

	// Cross-major migration is never supported.
	if sourceMajor != targetMajor {
		return nil, &ValidationError{
			Field:  "v",
			Reason: fmt.Sprintf("no migration path from major version %d to %d", sourceMajor, targetMajor),
			Major:  targetMajor,
		}
	}

	// Cross-version within the same major needs explicit rules.
	if env.Version != targetVersion {
		// No rules defined yet for any cross-version migration within v0.x.
		return nil, &ValidationError{
			Field:  "v",
			Reason: fmt.Sprintf("no migration rules from %s to %s", env.Version, targetVersion),
			Major:  targetMajor,
		}
	}

	// 2. Deep copy.
	out := Copy(env)

	// 3. Run defined migration steps (none for same-version).
	out.Version = targetVersion

	// 4. Recompute integrity hash.
	out.Integrity = IntegrityMetadata{
		ContentHash: ComputeIntegrityHash(out),
		Generator:   env.Integrity.Generator,
		GeneratedAt: env.Integrity.GeneratedAt,
	}

	// 5. Validate target envelope.
	if err := Validate(out); err != nil {
		return nil, fmt.Errorf("lui: migrated envelope invalid: %w", err)
	}

	return out, nil
}

// Copy returns a deep-ish copy (slices/map cloned) of the envelope safe for
// mutation.
func Copy(env *Envelope) *Envelope {
	if env == nil {
		return nil
	}
	out := *env
	out.Goals = append([]Goal{}, env.Goals...)
	out.Constraints = append([]Constraint{}, env.Constraints...)
	out.Context = append([]ContextReference{}, env.Context...)
	out.State = append([]StateEntry{}, env.State...)
	out.Tools = append([]ToolReference{}, env.Tools...)
	out.Evidence = append([]EvidenceReference{}, env.Evidence...)
	if env.Output.Fields != nil {
		out.Output.Fields = append([]string{}, env.Output.Fields...)
	}
	if env.Dictionary != nil {
		out.Dictionary = map[string]string{}
		for k, v := range env.Dictionary {
			out.Dictionary[k] = v
		}
	}
	return &out
}
