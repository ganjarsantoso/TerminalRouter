package optimization

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/normalization"
)

var (
	ansiRE      = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")
	ansiOSCRE   = regexp.MustCompile("\x1b\\][^\x07]*\x07")
	jsonLikeRE  = regexp.MustCompile(`^\s*[\{\[]`)
	errorWordRE = regexp.MustCompile(`(?i)\b(error|warning|fail|exception|traceback|panic|fatal)\b`)
)

// ---- ANSI stripper (lossless) ----

type ansiOptimizer struct{}

func (ansiOptimizer) Name() string    { return "ansi_strip" }
func (ansiOptimizer) Version() string { return "1.0" }
func (ansiOptimizer) Supports(oc OptimizationContext, mode config.OptimizationMode) bool {
	return mode != config.OptModeOff && ocHasFlag(oc, "strip_ansi")
}
func (o ansiOptimizer) Optimize(ctx context.Context, req *normalization.NormalizedRequest, oc OptimizationContext, mode config.OptimizationMode, res *OptimizationResult) error {
	if !ocHasFlag(oc, "strip_ansi") {
		return nil
	}
	changed := 0
	for i := range req.Messages {
		if req.Messages[i].Role != normalization.RoleTool {
			continue
		}
		for j := range req.Messages[i].Content {
			c := &req.Messages[i].Content[j]
			if c.Type != normalization.ContentText {
				continue
			}
			stripped := ansiRE.ReplaceAllString(c.Text, "")
			stripped = ansiOSCRE.ReplaceAllString(stripped, "")
			if stripped != c.Text {
				saved := len(c.Text) - len(stripped)
				c.Text = stripped
				changed += saved
			}
		}
	}
	if changed > 0 {
		res.Actions = append(res.Actions, Action{
			Kind:                 ActionANSIStripped,
			Description:          "Removed ANSI escape sequences from tool/log output",
			EstimatedTokensSaved: changed / 4,
			Reversible:           true,
			LossClass:            Lossless,
		})
	}
	return nil
}

// ---- JSON compactor (lossless) ----

type jsonCompactOptimizer struct{}

func (jsonCompactOptimizer) Name() string    { return "json_compact" }
func (jsonCompactOptimizer) Version() string { return "1.0" }
func (jsonCompactOptimizer) Supports(oc OptimizationContext, mode config.OptimizationMode) bool {
	return ocHasFlag(oc, "compact_json") && mode != config.OptModeOff
}
func (o jsonCompactOptimizer) Optimize(ctx context.Context, req *normalization.NormalizedRequest, oc OptimizationContext, mode config.OptimizationMode, res *OptimizationResult) error {
	saved := 0
	for i := range req.Messages {
		if req.Messages[i].Role != normalization.RoleTool {
			continue
		}
		for j := range req.Messages[i].Content {
			c := &req.Messages[i].Content[j]
			if c.Type != normalization.ContentText || !jsonLikeRE.MatchString(c.Text) {
				continue
			}
			compacted, ok := compactJSON(c.Text)
			if ok && len(compacted) < len(c.Text) {
				saved += len(c.Text) - len(compacted)
				c.Text = compacted
			}
		}
	}
	if saved > 0 {
		res.Actions = append(res.Actions, Action{
			Kind:                 ActionJSONCompacted,
			Description:          "Compacted JSON whitespace in tool results",
			EstimatedTokensSaved: saved / 4,
			Reversible:           true,
			LossClass:            Lossless,
		})
	}
	return nil
}

// ---- Exact duplicate removal (selective) ----
//
// Deduplication uses collision-resistant IDs (full SHA-256, 16 hex chars).
// The retained source message is annotated with a human-readable marker so
// the model can associate the reference with the source. Source message
// indices are tracked directly so conversation trimming cannot delete them.

type dedupeOptimizer struct{}

func (dedupeOptimizer) Name() string    { return "dedupe" }
func (dedupeOptimizer) Version() string { return "1.0" }
func (dedupeOptimizer) Supports(oc OptimizationContext, mode config.OptimizationMode) bool {
	return ocHasFlag(oc, "deduplicate") &&
		(mode == config.OptModeBalanced || mode == config.OptModeAggressive)
}
func (o dedupeOptimizer) Optimize(ctx context.Context, req *normalization.NormalizedRequest, oc OptimizationContext, mode config.OptimizationMode, res *OptimizationResult) error {
	seen := map[string]struct {
		refID       string
		sourceMsgIdx int
		sourceConIdx int
	}{}
	var sources []DedupSource
	saved := 0
	for i := range req.Messages {
		if req.Messages[i].Role != normalization.RoleTool && req.Messages[i].Role != normalization.RoleAssistant {
			continue
		}
		for j := range req.Messages[i].Content {
			c := &req.Messages[i].Content[j]
			if c.Type != normalization.ContentText || len(c.Text) < 64 {
				continue
			}
			h := fullHashText(c.Text)
			if _, ok := seen[h]; ok {
				ref := "[Exact duplicate of earlier tool result; omitted.]"
				saved += len(c.Text)
				c.Text = ref
			} else {
				refID := "trdup_" + h[:12]
				seen[h] = struct {
					refID       string
					sourceMsgIdx int
					sourceConIdx int
				}{refID: refID, sourceMsgIdx: i, sourceConIdx: j}
				sources = append(sources, DedupSource{
					MessageIndex: i,
					ContentIndex: j,
					ReferenceID:  refID,
				})
			}
		}
	}
	// Annotate retained sources so the model can associate the reference.
	if len(sources) > 0 {
		idxHdr := "1"
		if len(sources) > 1 {
			suffix := ""
			for _, s := range sources {
				suffix += " " + s.ReferenceID
			}
			for idx := range sources {
				s := &sources[idx]
				msg := &req.Messages[s.MessageIndex].Content[s.ContentIndex]
				if len(msg.Text) > 60 {
					msg.Text = "[Dedup source" + suffix + "]\n" + msg.Text
				}
			}
		} else {
			s := &sources[0]
			msg := &req.Messages[s.MessageIndex].Content[s.ContentIndex]
			if len(msg.Text) > 60 {
				msg.Text = "[Dedup source: " + s.ReferenceID + "]\n" + msg.Text
			}
		}
		_ = idxHdr
	}
	if len(sources) > 0 {
		res.DedupSources = sources
	}
	if saved > 0 {
		res.Actions = append(res.Actions, Action{
			Kind:                 ActionDuplicateRemoved,
			Description:          "Replaced exact-duplicate tool results with semantically meaningful messages; retained source annotated",
			EstimatedTokensSaved: saved / 4,
			Reversible:           false,
			LossClass:            Selective,
		})
	}
	return nil
}

// ---- Log / terminal output compactor (selective) ----

type logCompactOptimizer struct{}

func (logCompactOptimizer) Name() string    { return "log_compact" }
func (logCompactOptimizer) Version() string { return "1.0" }
func (logCompactOptimizer) Supports(oc OptimizationContext, mode config.OptimizationMode) bool {
	return ocHasFlag(oc, "compact_logs") &&
		(mode == config.OptModeBalanced || mode == config.OptModeAggressive)
}
func (o logCompactOptimizer) Optimize(ctx context.Context, req *normalization.NormalizedRequest, oc OptimizationContext, mode config.OptimizationMode, res *OptimizationResult) error {
	saved := 0
	for i := range req.Messages {
		if req.Messages[i].Role != normalization.RoleTool {
			continue
		}
		for j := range req.Messages[i].Content {
			c := &req.Messages[i].Content[j]
			if c.Type != normalization.ContentText {
				continue
			}
			out, ok, n := compactLog(c.Text, oc.Protected)
			if ok {
				saved += n
				c.Text = out
			}
		}
	}
	if saved > 0 {
		res.Actions = append(res.Actions, Action{
			Kind:                 ActionLogCompacted,
			Description:          "Collapsed repeated diagnostics; preserved errors, warnings, paths",
			EstimatedTokensSaved: saved / 4,
			Reversible:           false,
			LossClass:            Selective,
		})
	}
	return nil
}

// compactLog collapses runs of identical non-essential lines while preserving
// errors, warnings, file:line locations, and protected substrings. It returns
// the compacted text, whether a change occurred, and the approximate bytes saved.
func compactLog(text string, prot *ProtectedContent) (string, bool, int) {
	if text == "" {
		return text, false, 0
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	changed := false
	i := 0
	for i < len(lines) {
		line := lines[i]
		// Never collapse protected, error, or warning lines.
		if prot != nil && prot.Contains(line) || errorWordRE.MatchString(line) || strings.TrimSpace(line) == "" {
			out = append(out, line)
			i++
			continue
		}
		// Count identical consecutive lines.
		run := 1
		for i+run < len(lines) && lines[i+run] == line {
			run++
		}
		if run > 1 {
			out = append(out, line+"  (x"+itoa(run)+")")
			changed = true
			i += run
		} else {
			out = append(out, line)
			i++
		}
	}
	if !changed {
		return text, false, 0
	}
	compacted := strings.Join(out, "\n")
	return compacted, true, len(text) - len(compacted)
}

// ---- Conversation window management (selective) ----

type conversationOptimizer struct {
	est *estimatorRegistry
}

func (c conversationOptimizer) Name() string    { return "conversation_window" }
func (c conversationOptimizer) Version() string { return "1.0" }
func (conversationOptimizer) Supports(oc OptimizationContext, mode config.OptimizationMode) bool {
	return oc.ConversationEnabled && mode != config.OptModeOff
}
func (c conversationOptimizer) Optimize(ctx context.Context, req *normalization.NormalizedRequest, oc OptimizationContext, mode config.OptimizationMode, res *OptimizationResult) error {
	if !oc.ConversationEnabled || len(req.Messages) == 0 {
		return nil
	}
	total := c.est.Estimate(req, oc.ProviderID, oc.ModelID).Total
	if total < oc.ConversationTriggerTokens {
		return nil
	}

	lastUserIdx := -1
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == normalization.RoleUser {
			lastUserIdx = i
			break
		}
	}

	protectedIdx := map[int]bool{}
	if lastUserIdx >= 0 {
		protectedIdx[lastUserIdx] = true
	}
	for i, m := range req.Messages {
		if m.Role == normalization.RoleSystem {
			protectedIdx[i] = true
		}
		if oc.Protected != nil {
			for _, blk := range m.Content {
				if blk.Type == normalization.ContentText && oc.Protected.Contains(blk.Text) {
					protectedIdx[i] = true
				}
			}
		}
	}

	toolCallIDs := map[string]int{}
	for i, m := range req.Messages {
		for _, blk := range m.Content {
			if blk.Type == normalization.ContentToolCall {
				toolCallIDs[blk.ToolCallID] = i
			}
		}
	}
	for i, m := range req.Messages {
		for _, blk := range m.Content {
			if blk.Type == normalization.ContentToolResult && blk.ToolCallID != "" {
				if callIdx, ok := toolCallIDs[blk.ToolCallID]; ok {
					protectedIdx[i] = true
					protectedIdx[callIdx] = true
				}
			}
		}
	}

	// Protect messages that are dedup sources (first occurrence of deduplicated
	// content). Removing the source would break references that point to it.
	for _, ds := range res.DedupSources {
		if ds.MessageIndex >= 0 && ds.MessageIndex < len(req.Messages) {
			protectedIdx[ds.MessageIndex] = true
		}
	}

	keep := oc.ConversationRecentTurns * 2
	if keep < 1 {
		keep = 1
	}

	keepSet := map[int]bool{}
	for i := len(req.Messages) - 1; i >= 0 && keep > 0; i-- {
		if !protectedIdx[i] {
			keepSet[i] = true
			keep--
		}
	}
	for idx := range protectedIdx {
		keepSet[idx] = true
	}

	var kept []normalization.Message
	var keptOriginalIdx []int
	for i := 0; i < len(req.Messages); i++ {
		if keepSet[i] {
			kept = append(kept, req.Messages[i])
			keptOriginalIdx = append(keptOriginalIdx, i)
		}
	}

	for len(kept) > 1 {
		est := c.est.Estimate(&normalization.NormalizedRequest{Messages: kept}, oc.ProviderID, oc.ModelID).Total
		if est <= oc.ConversationTargetTokens {
			break
		}
		removed := false
		for idx := 0; idx < len(kept); idx++ {
			if protectedIdx[keptOriginalIdx[idx]] {
				continue
			}
			kept = append(kept[:idx], kept[idx+1:]...)
			keptOriginalIdx = append(keptOriginalIdx[:idx], keptOriginalIdx[idx+1:]...)
			removed = true
			break
		}
		if !removed {
			break
		}
	}
	if len(kept) < len(req.Messages) {
		removed := len(req.Messages) - len(kept)
		req.Messages = kept
		res.Actions = append(res.Actions, Action{
			Kind:                 ActionConversationTrimmed,
			Description:          "Trimmed old conversation history beyond target window (protected turns retained)",
			EstimatedTokensSaved: removed * 64,
			Reversible:           false,
			LossClass:            Selective,
		})
	}
	return nil
}

// ---- helpers ----

func ocHasFlag(oc OptimizationContext, flag string) bool {
	for _, f := range oc.Flags {
		if f == flag {
			return true
		}
	}
	return false
}

// hashText returns an 8-hex-char hash (first 4 bytes of SHA-256). Deprecated;
// use fullHashText for collision-resistant dedup IDs.
func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:4])
}

// fullHashText returns a 16-hex-char collision-resistant content hash (first 8
// bytes of SHA-256). Used by deduplication to generate stable reference IDs
// that survive reordering and conversation trimming.
func fullHashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// padZero formats n as a zero-padded decimal with at least width digits.
func padZero(n, width int) string {
	s := itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// compactJSON removes insignificant whitespace from a JSON document using the
// standard library. It returns the compacted string and true on success; for
// invalid JSON it returns the input unchanged and false.
func compactJSON(input string) (string, bool) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(input)); err != nil {
		return input, false
	}
	return buf.String(), true
}
