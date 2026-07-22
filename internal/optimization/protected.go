package optimization

import (
	"strings"

	"github.com/termrouter/termrouter/internal/normalization"
)

// ProtectedContent records the regions of a request that optimizers must never
// modify. It is built before any transformation runs.
type ProtectedContent struct {
	SystemProtected       bool
	CurrentUserProtected  bool
	ToolSchemasProtected  bool
	ProtectedToolNames    map[string]bool
	ProtectedToolChoice   bool
	ProtectedResponseFmt  bool
	ProtectedStopSeqs     bool
	ProtectedMetadataKeys map[string]bool
	ToolCallIDs           map[string]bool
	ToolResultIDs         map[string]bool
	// ProtectedSubstrings are exact substrings (e.g. error file/line locations,
	// patch diffs) that compactors must preserve verbatim.
	ProtectedSubstrings []string
}

// BuildProtected derives the protected-content map from the normalized request.
// The following regions are protected:
//   - system instructions (immutable per §3.1)
//   - current user message (lossless transforms only)
//   - full tool schemas: name, description, and input_schema
//   - tool choice and parallel-tool constraints
//   - response format / schema
//   - stop sequences where protected by contract
//   - tool-call IDs and tool-result IDs (referential integrity)
//   - client policy metadata relevant to provider execution
func BuildProtected(req *normalization.NormalizedRequest) *ProtectedContent {
	p := &ProtectedContent{
		SystemProtected:       req.System != "",
		CurrentUserProtected:  true,
		ToolSchemasProtected:  true,
		ProtectedToolNames:    map[string]bool{},
		ProtectedToolChoice:   req.ToolChoice != nil,
		ProtectedResponseFmt:  req.ResponseFormat != nil,
		ProtectedStopSeqs:     len(req.StopSequences) > 0,
		ProtectedMetadataKeys: map[string]bool{},
		ToolCallIDs:           map[string]bool{},
		ToolResultIDs:         map[string]bool{},
	}
	for _, t := range req.Tools {
		p.ProtectedToolNames[t.Name] = true
	}
	// Collect tool-call IDs and tool-result IDs from messages to protect
	// referential integrity between calls and results.
	for _, m := range req.Messages {
		for _, c := range m.Content {
			if c.ToolCallID != "" {
				if c.Type == normalization.ContentToolCall {
					p.ToolCallIDs[c.ToolCallID] = true
				}
				if c.Type == normalization.ContentToolResult {
					p.ToolResultIDs[c.ToolCallID] = true
				}
			}
		}
	}
	// Protect known client-policy metadata keys that affect provider execution.
	if req.Metadata != nil {
		for k := range req.Metadata {
			switch k {
			case "client_policy", "provider_hints", "execution_constraints",
				"parallel_tool_calls", "user_id", "session_id",
				"allowed_providers", "blocked_providers":
				p.ProtectedMetadataKeys[k] = true
			}
		}
	}
	// Preserve file:line style locations and diff markers found in tool results.
	for _, m := range req.Messages {
		if m.Role != normalization.RoleTool {
			continue
		}
		for _, c := range m.Content {
			p.ProtectedSubstrings = append(p.ProtectedSubstrings, extractLocations(c.Text)...)
		}
	}
	return p
}

// Contains reports whether text includes a protected substring.
func (p *ProtectedContent) Contains(text string) bool {
	for _, s := range p.ProtectedSubstrings {
		if s != "" && strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// IsProtectedMessage reports whether a message index holds protected content
// that must not be altered by deterministic compactors.
func (p *ProtectedContent) IsProtectedMessage(m normalization.Message) bool {
	if m.Role == normalization.RoleUser {
		return p.CurrentUserProtected
	}
	if m.Role == normalization.RoleSystem {
		return p.SystemProtected
	}
	return false
}
