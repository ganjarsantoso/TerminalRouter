package lui

// TaskDescriptor describes the primary task of a packet.
type TaskDescriptor struct {
	Type       string `json:"type"`
	Complexity string `json:"complexity,omitempty"`
	Summary    string `json:"summary,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
}

// Goal is a structured objective extracted from the request.
type Goal struct {
	Type     string `json:"type"`
	Summary  string `json:"summary,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Source   Source `json:"source,omitempty"`
}

// Constraint is a structured, provenance-tracked restriction or requirement.
type Constraint struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Value      string          `json:"value"`
	Priority   int             `json:"priority,omitempty"`
	Source     Source          `json:"source"`
	Mutable    bool            `json:"mutable,omitempty"`
	Protection ProtectionClass `json:"protection,omitempty"`
}

// ContextReference points to a piece of context (inline or by URI).
type ContextReference struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind,omitempty"`
	URI           string          `json:"uri,omitempty"`
	ContentHash   string          `json:"content_hash,omitempty"`
	TokenEstimate int             `json:"token_estimate,omitempty"`
	Priority      int             `json:"priority,omitempty"`
	Protection    ProtectionClass `json:"protection,omitempty"`
	Inline        bool            `json:"inline,omitempty"`
	Content       string          `json:"content,omitempty"`
}

// StateEntry is a single key/value of agent or task state.
type StateEntry struct {
	Key        string          `json:"key"`
	Value      string          `json:"value"`
	Source     Source          `json:"source"`
	Protection ProtectionClass `json:"protection,omitempty"`
}

// ToolReference references a tool used by the request.
type ToolReference struct {
	Name       string `json:"name"`
	SchemaHash string `json:"schema_hash,omitempty"`
	Source     Source `json:"source"`
}

// EvidenceReference references an evidence or retrieved document.
type EvidenceReference struct {
	ID      string `json:"id"`
	Kind    string `json:"kind,omitempty"`
	URI     string `json:"uri,omitempty"`
	Summary string `json:"summary,omitempty"`
	Source  Source `json:"source"`
}

// OutputContract describes the expected output shape.
type OutputContract struct {
	Format string   `json:"format,omitempty"`
	Fields []string `json:"fields,omitempty"`
}

// IntegrityMetadata records envelope integrity and generation metadata.
type IntegrityMetadata struct {
	ContentHash string `json:"content_hash,omitempty"`
	GeneratedAt string `json:"generated_at,omitempty"`
	Generator   string `json:"generator,omitempty"`
}

// Envelope is the LUI v0.1 semantic packet.
type Envelope struct {
	Version     string              `json:"v"`
	Kind        PacketKind          `json:"kind"`
	Task        TaskDescriptor      `json:"task"`
	Goals       []Goal              `json:"goals,omitempty"`
	Constraints []Constraint        `json:"constraints,omitempty"`
	Context     []ContextReference  `json:"context,omitempty"`
	State       []StateEntry        `json:"state,omitempty"`
	Tools       []ToolReference     `json:"tools,omitempty"`
	Evidence    []EvidenceReference `json:"evidence,omitempty"`
	Output      OutputContract      `json:"output,omitempty"`
	Dictionary  map[string]string   `json:"dict,omitempty"`
	Integrity   IntegrityMetadata   `json:"integrity,omitempty"`
}
