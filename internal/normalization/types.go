package normalization

// Role is a message role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentType identifies a content block.
type ContentType string

const (
	ContentText       ContentType = "text"
	ContentImage      ContentType = "image"
	ContentToolCall   ContentType = "tool_call"
	ContentToolResult ContentType = "tool_result"
	ContentReasoning  ContentType = "reasoning"
)

// ContentBlock is a unit of message content.
type ContentBlock struct {
	Type       ContentType    `json:"type"`
	Text       string         `json:"text,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	Arguments  string         `json:"arguments,omitempty"` // JSON string
	IsError    bool           `json:"is_error,omitempty"`
	ImageURL   string         `json:"image_url,omitempty"`
	MimeType   string         `json:"mime_type,omitempty"`
	Extra      map[string]any `json:"extra,omitempty"`
}

// Message is a chat message.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
	Name    string         `json:"name,omitempty"`
}

// Tool defines a callable function.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

// ToolChoice controls tool use.
type ToolChoice struct {
	Type string `json:"type"` // auto | none | required | tool
	Name string `json:"name,omitempty"`
}

// NormalizedRequest is the provider-neutral request representation.
type NormalizedRequest struct {
	ID                 string         `json:"id"`
	RequestedModel     string         `json:"requested_model"`
	ResolvedAlias      string         `json:"resolved_alias,omitempty"`
	Messages           []Message      `json:"messages"`
	Tools              []Tool         `json:"tools,omitempty"`
	ToolChoice         *ToolChoice    `json:"tool_choice,omitempty"`
	Temperature        *float64       `json:"temperature,omitempty"`
	TopP               *float64       `json:"top_p,omitempty"`
	MaxOutputTokens    *int           `json:"max_output_tokens,omitempty"`
	StopSequences      []string       `json:"stop_sequences,omitempty"`
	ResponseFormat     map[string]any `json:"response_format,omitempty"`
	Stream             bool           `json:"stream"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	ProviderOptions    map[string]any `json:"provider_options,omitempty"`
	RequiredCapabilities []string     `json:"required_capabilities,omitempty"`
	// System is extracted system text (also present in messages when needed).
	System string `json:"system,omitempty"`
}

// StopReason is the internal stop reason.
type StopReason string

const (
	StopEndTurn      StopReason = "end_turn"
	StopMaxTokens    StopReason = "max_tokens"
	StopToolUse      StopReason = "tool_use"
	StopBlocked      StopReason = "blocked"
	StopSequence     StopReason = "stop_sequence"
	StopError        StopReason = "error"
)

// Usage holds token counts.
type Usage struct {
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	Source       string `json:"source"` // provider_reported | estimated
}

// NormalizedResponse is a complete non-streaming response.
type NormalizedResponse struct {
	ID           string         `json:"id"`
	Model        string         `json:"model"` // public model name
	UpstreamModel string        `json:"upstream_model,omitempty"`
	ProviderID   string         `json:"provider_id,omitempty"`
	Content      []ContentBlock `json:"content"`
	StopReason   StopReason     `json:"stop_reason"`
	Usage        Usage          `json:"usage"`
	Raw          map[string]any `json:"raw,omitempty"`
}

// EventType for streaming.
type EventType string

const (
	EventMessageStart      EventType = "message_start"
	EventContentBlockStart EventType = "content_block_start"
	EventTextDelta         EventType = "text_delta"
	EventToolCallStart     EventType = "tool_call_start"
	EventToolCallDelta     EventType = "tool_call_delta"
	EventReasoningDelta    EventType = "reasoning_delta"
	EventContentBlockStop  EventType = "content_block_stop"
	EventUsageDelta        EventType = "usage_delta"
	EventMessageStop       EventType = "message_stop"
	EventError             EventType = "error"
)

// StreamEvent is a normalized streaming event.
type StreamEvent struct {
	Type       EventType      `json:"type"`
	Index      int            `json:"index,omitempty"`
	Text       string         `json:"text,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	Arguments  string         `json:"arguments,omitempty"`
	StopReason StopReason     `json:"stop_reason,omitempty"`
	Usage      *Usage         `json:"usage,omitempty"`
	Error      *Error         `json:"error,omitempty"`
	// Commit marks client-visible semantic content (text/tool start).
	Commit bool `json:"-"`
}

// Error is a normalized API error.
type Error struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status"`
	Retryable  bool   `json:"retryable"`
	Provider   string `json:"provider,omitempty"`
}

// Error codes from PRD §10.4.
const (
	ErrAuthentication     = "authentication_error"
	ErrPermissionDenied   = "permission_denied"
	ErrInvalidRequest     = "invalid_request"
	ErrUnsupportedFeature = "unsupported_feature"
	ErrModelNotFound      = "model_not_found"
	ErrRateLimited        = "rate_limited"
	ErrProviderAuth       = "provider_auth_error"
	ErrProviderUnavailable = "provider_unavailable"
	ErrUpstreamTimeout    = "upstream_timeout"
	ErrInternal           = "internal_error"
)

func NewError(code, message string, httpStatus int) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: httpStatus}
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// MapStopToOpenAI maps internal stop reasons to OpenAI finish_reason.
func MapStopToOpenAI(s StopReason) string {
	switch s {
	case StopMaxTokens:
		return "length"
	case StopToolUse:
		return "tool_calls"
	case StopBlocked:
		return "content_filter"
	case StopError:
		return "stop"
	default:
		return "stop"
	}
}

// MapStopToAnthropic maps internal stop reasons to Anthropic stop_reason.
func MapStopToAnthropic(s StopReason) string {
	switch s {
	case StopMaxTokens:
		return "max_tokens"
	case StopToolUse:
		return "tool_use"
	case StopBlocked:
		return "refusal"
	case StopSequence:
		return "stop_sequence"
	case StopError:
		return "end_turn"
	default:
		return "end_turn"
	}
}

// MapOpenAIStop maps OpenAI finish_reason to internal.
func MapOpenAIStop(s string) StopReason {
	switch s {
	case "length":
		return StopMaxTokens
	case "tool_calls", "function_call":
		return StopToolUse
	case "content_filter":
		return StopBlocked
	default:
		return StopEndTurn
	}
}

// MapAnthropicStop maps Anthropic stop_reason to internal.
func MapAnthropicStop(s string) StopReason {
	switch s {
	case "max_tokens":
		return StopMaxTokens
	case "tool_use":
		return StopToolUse
	case "refusal":
		return StopBlocked
	case "stop_sequence":
		return StopSequence
	default:
		return StopEndTurn
	}
}

// TextFromContent joins text blocks.
func TextFromContent(blocks []ContentBlock) string {
	var b stringsBuilder
	for _, c := range blocks {
		if c.Type == ContentText {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// minimal strings.Builder-like without import cycle concerns
type stringsBuilder struct {
	s string
}

func (b *stringsBuilder) WriteString(s string) {
	b.s += s
}

func (b *stringsBuilder) String() string { return b.s }
