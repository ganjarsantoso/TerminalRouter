package lui

import "sort"

// canonicalEnvelope is a deterministic serialization structure for integrity
// hashing. It excludes Integrity.ContentHash, GeneratedAt, and Generator.
type canonicalEnvelope struct {
	Version     string
	Kind        PacketKind
	Task        TaskDescriptor
	Goals       []Goal
	Constraints []Constraint
	Context     []ContextReference
	State       []StateEntry
	Tools       []ToolReference
	Evidence    []EvidenceReference
	Output      OutputContract
	Dictionary  []canonicalDictionaryEntry
}

type canonicalDictionaryEntry struct {
	Key   string
	Value string
}

// toCanonical converts an envelope to its canonical form for integrity hashing.
//
// Contract — order-insensitive collections:
// All multi-value semantic collections are treated as unordered multisets.
// They are sorted with a deterministic total order over identifying fields
// (and additional fields when needed for a total order) so that reordering
// entries never changes the integrity hash. Collections covered:
//
//   - Goals           (priority desc, then type, summary, source)
//   - Constraints     (id, then type, value, priority, source, mutable, protection)
//   - Context         (id, then kind, uri, content_hash, …)
//   - State           (key, then value, source, protection)
//   - Tools           (name, then schema_hash, source)
//   - Evidence        (id, then kind, uri, summary, source)
//   - Output.Fields   (lexicographic)
//   - Dictionary keys (lexicographic; map → sorted pairs)
//
// Scalar and non-collection fields (Task, Output.Format, Version, Kind) keep
// their natural values and are not reordered. Do not describe individual
// collections as order-sensitive unless this contract is deliberately changed.
func toCanonical(env *Envelope) canonicalEnvelope {
	c := canonicalEnvelope{
		Version:     env.Version,
		Kind:        env.Kind,
		Task:        env.Task,
		Goals:       append([]Goal{}, env.Goals...),
		Constraints: append([]Constraint{}, env.Constraints...),
		Context:     append([]ContextReference{}, env.Context...),
		State:       append([]StateEntry{}, env.State...),
		Tools:       append([]ToolReference{}, env.Tools...),
		Evidence:    append([]EvidenceReference{}, env.Evidence...),
		Output:      env.Output,
	}
	if env.Output.Fields != nil {
		c.Output.Fields = append([]string{}, env.Output.Fields...)
	}

	sort.Slice(c.Goals, func(i, j int) bool {
		a, b := c.Goals[i], c.Goals[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Summary != b.Summary {
			return a.Summary < b.Summary
		}
		return a.Source < b.Source
	})
	sort.Slice(c.Constraints, func(i, j int) bool {
		a, b := c.Constraints[i], c.Constraints[j]
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Value != b.Value {
			return a.Value < b.Value
		}
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Mutable != b.Mutable {
			return !a.Mutable && b.Mutable
		}
		return a.Protection < b.Protection
	})
	sort.Slice(c.Context, func(i, j int) bool {
		a, b := c.Context[i], c.Context[j]
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.URI != b.URI {
			return a.URI < b.URI
		}
		if a.ContentHash != b.ContentHash {
			return a.ContentHash < b.ContentHash
		}
		if a.TokenEstimate != b.TokenEstimate {
			return a.TokenEstimate < b.TokenEstimate
		}
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if a.Protection != b.Protection {
			return a.Protection < b.Protection
		}
		if a.Inline != b.Inline {
			return !a.Inline && b.Inline
		}
		return a.Content < b.Content
	})
	sort.Slice(c.State, func(i, j int) bool {
		a, b := c.State[i], c.State[j]
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		if a.Value != b.Value {
			return a.Value < b.Value
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Protection < b.Protection
	})
	sort.Slice(c.Tools, func(i, j int) bool {
		a, b := c.Tools[i], c.Tools[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.SchemaHash != b.SchemaHash {
			return a.SchemaHash < b.SchemaHash
		}
		return a.Source < b.Source
	})
	sort.Slice(c.Evidence, func(i, j int) bool {
		a, b := c.Evidence[i], c.Evidence[j]
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.URI != b.URI {
			return a.URI < b.URI
		}
		if a.Summary != b.Summary {
			return a.Summary < b.Summary
		}
		return a.Source < b.Source
	})
	sort.Slice(c.Output.Fields, func(i, j int) bool {
		return c.Output.Fields[i] < c.Output.Fields[j]
	})

	keys := make([]string, 0, len(env.Dictionary))
	for k := range env.Dictionary {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c.Dictionary = append(c.Dictionary, canonicalDictionaryEntry{Key: k, Value: env.Dictionary[k]})
	}

	return c
}
