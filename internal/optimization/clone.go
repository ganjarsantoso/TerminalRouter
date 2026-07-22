package optimization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/termrouter/termrouter/internal/normalization"
)

// CloneRequest performs a deep copy of a NormalizedRequest so that optimizers
// can mutate the clone without affecting the authoritative request. The clone
// uses JSON round-trip for simplicity and correctness; all fields in
// NormalizedRequest are serializable.
func CloneRequest(req *normalization.NormalizedRequest) *normalization.NormalizedRequest {
	if req == nil {
		return nil
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil
	}
	var clone normalization.NormalizedRequest
	if err := json.Unmarshal(b, &clone); err != nil {
		return nil
	}
	return &clone
}

// ProtectedFingerprint returns a compact fingerprint of the truly immutable
// content regions that must never change across any optimization stage:
//   - system text (system instructions are immutable per §3.1)
//   - tool names, descriptions, and full input schemas (tools are protected per §3.1)
//   - tool choice constraints (type and name)
//   - response format / schema (structured output contract)
//   - stop sequences (boundary contract)
//   - current user request text (lossless transforms only)
//
// Note: the current user message text IS included because Item #26 requires
// covering the user request; only transformations explicitly proven safe and
// allowed are permitted to modify it.
func ProtectedFingerprint(req *normalization.NormalizedRequest) string {
	if req == nil {
		return ""
	}
	h := sha256.New()
	h.Write([]byte(req.System))
	for _, t := range req.Tools {
		h.Write([]byte(t.Name))
		h.Write([]byte(t.Description))
		if t.InputSchema != nil {
			schemaJSON, _ := json.Marshal(t.InputSchema)
			h.Write(schemaJSON)
		}
	}
	// Tool choice constraints
	if req.ToolChoice != nil {
		tcJSON, _ := json.Marshal(req.ToolChoice)
		h.Write(tcJSON)
	}
	// Response format / schema
	if req.ResponseFormat != nil {
		rfJSON, _ := json.Marshal(req.ResponseFormat)
		h.Write(rfJSON)
	}
	// Stop sequences (sorted for determinism)
	if len(req.StopSequences) > 0 {
		sorted := append([]string{}, req.StopSequences...)
		sort.Strings(sorted)
		for _, s := range sorted {
			h.Write([]byte(s))
		}
	}
	// Current user request text (only the last user turn)
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == normalization.RoleUser {
			for _, c := range req.Messages[i].Content {
				if c.Type == normalization.ContentText {
					h.Write([]byte(c.Text))
				}
			}
			break
		}
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}
