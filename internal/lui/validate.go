package lui

import (
	"errors"
	"fmt"
)

// ValidationError is returned when an envelope fails validation.
type ValidationError struct {
	Field   string
	Reason  string
	Major   int
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("lui: %s", e.Message)
	}
	return fmt.Sprintf("lui: field %q: %s", e.Field, e.Reason)
}

// IsMajorVersionError reports whether err indicates an unsupported major version
// (the caller should fall back to a safe native rendering).
func IsMajorVersionError(err error) bool {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve.Major > 0
	}
	return false
}

func majorOf(v string) int {
	var major int
	_, _ = fmt.Sscanf(v, "%d.", &major)
	return major
}

// Validate checks an envelope for structural and provenance correctness.
// It rejects missing version/kind, unknown major versions, invalid sources,
// invalid protection classes, and unresolved dictionary references.
func Validate(env *Envelope) error {
	if env == nil {
		return &ValidationError{Message: "nil envelope"}
	}
	if env.Version == "" {
		return &ValidationError{Field: "v", Reason: "version is required"}
	}
	major := majorOf(env.Version)
	if major != SupportedMajor {
		return &ValidationError{Field: "v", Reason: fmt.Sprintf("unsupported major version %d (supported: %d)", major, SupportedMajor), Major: major}
	}
	if env.Kind == "" {
		return &ValidationError{Field: "kind", Reason: "packet kind is required"}
	}
	switch env.Kind {
	case KindTask, KindStateUpdate, KindFindingSet, KindExecutionPlan, KindToolResult,
		KindContextManifest, KindTestReport, KindCompletion, KindHandoff:
	default:
		return &ValidationError{Field: "kind", Reason: fmt.Sprintf("unknown packet kind %q", env.Kind)}
	}
	for i, c := range env.Constraints {
		if !ValidSource(c.Source) {
			return &ValidationError{Field: fmt.Sprintf("constraints[%d].source", i), Reason: fmt.Sprintf("invalid source %q", c.Source)}
		}
		if c.Protection != "" {
			switch c.Protection {
			case ProtectionImmutable, ProtectionProtected, ProtectionSummarizable, ProtectionOptional:
			default:
				return &ValidationError{Field: fmt.Sprintf("constraints[%d].protection", i), Reason: fmt.Sprintf("invalid protection %q", c.Protection)}
			}
		}
	}
	for i, s := range env.State {
		if !ValidSource(s.Source) {
			return &ValidationError{Field: fmt.Sprintf("state[%d].source", i), Reason: fmt.Sprintf("invalid source %q", s.Source)}
		}
	}
	for i, t := range env.Tools {
		if !ValidSource(t.Source) {
			return &ValidationError{Field: fmt.Sprintf("tools[%d].source", i), Reason: fmt.Sprintf("invalid source %q", t.Source)}
		}
	}
	for i, e := range env.Evidence {
		if !ValidSource(e.Source) {
			return &ValidationError{Field: fmt.Sprintf("evidence[%d].source", i), Reason: fmt.Sprintf("invalid source %q", e.Source)}
		}
	}
	for i, ref := range env.Context {
		if ref.Content == "" && ref.URI == "" {
			// Must resolve through the dictionary.
			if _, ok := env.Dictionary[ref.ID]; !ok {
				return &ValidationError{Field: fmt.Sprintf("context[%d]", i), Reason: fmt.Sprintf("reference %q has no inline content, URI, or dictionary entry", ref.ID)}
			}
		}
	}

	// Verify integrity hash when present.
	if env.Integrity.ContentHash != "" {
		if !VerifyIntegrityHash(env) {
			return &ValidationError{Field: "integrity.content_hash", Reason: "content hash mismatch — envelope has been tampered with"}
		}
	}

	return nil
}
