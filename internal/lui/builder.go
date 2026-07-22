package lui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/termrouter/termrouter/internal/normalization"
)

// BuildContext carries the inputs needed to construct an envelope.
type BuildContext struct {
	RequestID       string
	InboundProtocol string
	// ClientConstraints are explicit constraints supplied by the client in
	// TermRouter metadata (source = client_explicit).
	ClientConstraints []Constraint
	// TaskType overrides the inferred task type (e.g. from Smart Route).
	TaskType string
	// Complexity is the task complexity label from classification.
	Complexity string
}

// BuildEnvelope constructs a deterministic LUI v0.1 envelope from a normalized
// request. It does not invoke any model and never invents constraints. Tool
// references and the current user message are captured for structure only.
func BuildEnvelope(req *normalization.NormalizedRequest, bc BuildContext) *Envelope {
	env := &Envelope{
		Version: Version,
		Kind:    KindTask,
		Task: TaskDescriptor{
			Type:       bc.TaskType,
			Complexity: bc.Complexity,
			Summary:    summarizeRequest(req),
			RequestID:  bc.RequestID,
		},
		Constraints: append([]Constraint{}, bc.ClientConstraints...),
	}
	if env.Task.Type == "" {
		env.Task.Type = "chat"
	}

	for _, t := range req.Tools {
		env.Tools = append(env.Tools, ToolReference{
			Name:       t.Name,
			SchemaHash: hashString(t.Description + marshalSafe(t.InputSchema)),
			Source:     SourceClientExplicit,
		})
	}

	// Capture the latest user turn as a state entry for downstream agents.
	lastUser := lastUserText(req)
	if lastUser != "" {
		env.State = append(env.State, StateEntry{
			Key:        "current_user_request",
			Value:      lastUser,
			Source:     SourceClientExplicit,
			Protection: ProtectionProtected,
		})
	}

	// Preserve any system security instructions as immutable context.
	if req.System != "" {
		env.Context = append(env.Context, ContextReference{
			ID:         "system",
			Kind:       "system_instructions",
			Content:    req.System,
			Protection: ProtectionImmutable,
			Inline:     true,
		})
	}

	// Compute canonical integrity hash over ALL semantic fields, excluding the
	// ContentHash itself. Map keys are sorted for determinism.
	env.Integrity = IntegrityMetadata{
		ContentHash: ComputeIntegrityHash(env),
		Generator:   "termrouter/lui/" + Version,
	}
	return env
}

// ComputeIntegrityHash computes a deterministic SHA-256 hash over all semantic
// fields of the envelope using canonical JSON serialization. The ContentHash
// field itself is excluded from the hash input. Non-semantic fields
// (GeneratedAt, Generator) are also excluded.
//
// Collection fields (goals, constraints, context, state, tools, evidence,
// output fields, dictionary) are order-insensitive multisets: toCanonical
// sorts them with a total order before serialization, so reordering entries
// does not change the hash. See toCanonical for the per-collection sort keys.
func ComputeIntegrityHash(env *Envelope) string {
	if env == nil {
		return ""
	}
	c := toCanonical(env)
	data, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return hashString(string(data))
}

// VerifyIntegrityHash checks whether the envelope's stored ContentHash matches
// the recomputed hash. Returns true if valid, false if tampered.
func VerifyIntegrityHash(env *Envelope) bool {
	if env == nil || env.Integrity.ContentHash == "" {
		return false
	}
	return env.Integrity.ContentHash == ComputeIntegrityHash(env)
}

func summarizeRequest(req *normalization.NormalizedRequest) string {
	u := lastUserText(req)
	if len(u) > 200 {
		return u[:200] + "..."
	}
	return u
}

func lastUserText(req *normalization.NormalizedRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		m := req.Messages[i]
		if m.Role != normalization.RoleUser {
			continue
		}
		var b strings.Builder
		for _, c := range m.Content {
			if c.Type == normalization.ContentText {
				b.WriteString(c.Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return ""
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func marshalSafe(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
