package lui

// Version is the current LUI schema version (v0.1).
const Version = "0.1"

// SupportedMajor is the major version this implementation understands.
const SupportedMajor = 0

// PacketKind enumerates the LUI packet kinds.
type PacketKind string

const (
	KindTask            PacketKind = "task"
	KindStateUpdate     PacketKind = "state_update"
	KindFindingSet      PacketKind = "finding_set"
	KindExecutionPlan   PacketKind = "execution_plan"
	KindToolResult      PacketKind = "tool_result"
	KindContextManifest PacketKind = "context_manifest"
	KindTestReport      PacketKind = "test_report"
	KindCompletion      PacketKind = "completion_result"
	KindHandoff         PacketKind = "handoff"
)

// ProtectionClass controls how a field may be optimized.
type ProtectionClass string

const (
	ProtectionImmutable    ProtectionClass = "immutable"
	ProtectionProtected    ProtectionClass = "protected"
	ProtectionSummarizable ProtectionClass = "summarizable"
	ProtectionOptional     ProtectionClass = "optional"
)

// Source is the provenance of a constraint, goal, or state entry.
type Source string

const (
	SourceServerPolicy    Source = "server_policy"
	SourceClientKeyPolicy Source = "client_key_policy"
	SourceRoutePolicy     Source = "route_policy"
	SourceClientExplicit  Source = "client_explicit"
	SourceClientMetadata  Source = "client_metadata"
	SourceAgentGenerated  Source = "agent_generated"
	SourceModelInferred   Source = "model_inferred"
	SourceCompressorGen   Source = "compressor_generated"
)

// sourceAuthority orders provenance from highest (0) to lowest authority.
var sourceAuthority = map[Source]int{
	SourceServerPolicy:    0,
	SourceClientKeyPolicy: 1,
	SourceRoutePolicy:     2,
	SourceClientExplicit:  3,
	SourceClientMetadata:  4,
	SourceAgentGenerated:  5,
	SourceModelInferred:   6,
	SourceCompressorGen:   7,
}

// SourceRank returns the ordinal rank of a source (0 = highest authority,
// larger number = lower authority, 99 = unknown/lowest).
func SourceRank(s Source) int {
	if v, ok := sourceAuthority[s]; ok {
		return v
	}
	return 99
}

// ValidSource reports whether s is a recognized provenance source.
func ValidSource(s Source) bool {
	_, ok := sourceAuthority[s]
	return ok
}

// CanOverride reports whether a lower-authority source may override a
// higher-authority constraint of the same identity. Inferred/agent/compressor
// sources must never override server/key/route/explicit client constraints.
func CanOverride(overriding, existing Source) bool {
	return SourceRank(overriding) < SourceRank(existing)
}
