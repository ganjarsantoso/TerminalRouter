package lui

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RendererState is the verification status of a renderer for a model.
type RendererState string

const (
	RendererUnknown      RendererState = "unknown"
	RendererExperimental RendererState = "experimental"
	RendererSupported    RendererState = "supported"
	RendererVerified     RendererState = "verified"
	RendererPreferred    RendererState = "preferred"
	RendererBlocked      RendererState = "blocked"
)

// Renderer renders a LUI envelope into a wire representation.
type Renderer interface {
	Name() string
	Render(env *Envelope) (string, error)
}

var (
	rendererCompactJSON  = &compactJSONRenderer{}
	rendererHuman        = &humanRenderer{}
	rendererTagged       = &taggedRenderer{}
	rendererNativePrompt = &nativePromptRenderer{}
)

// Renderers returns the built-in renderers by name.
func Renderers() map[string]Renderer {
	return map[string]Renderer{
		"compact_json":  rendererCompactJSON,
		"human":         rendererHuman,
		"tagged_text":   rendererTagged,
		"native_prompt": rendererNativePrompt,
	}
}

// UnknownRendererError is returned when a renderer name is not recognized.
type UnknownRendererError struct {
	Name string
}

func (e *UnknownRendererError) Error() string {
	return fmt.Sprintf("lui: unknown renderer %q", e.Name)
}

// GetRenderer returns a renderer by name (case-insensitive, space-trimmed,
// hyphen/underscore interchangeable). The native prompt renderer is always
// available as a safe fallback.
func GetRenderer(name string) (Renderer, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return rendererNativePrompt, nil
	}
	name = strings.ReplaceAll(name, "-", "_")
	for k, r := range Renderers() {
		if k == name {
			return r, nil
		}
	}
	return nil, &UnknownRendererError{Name: name}
}

// Render renders the envelope with the requested renderer, falling back to the
// native prompt renderer when the requested one is unknown or invalid.
func Render(env *Envelope, rendererName string) (string, string, error) {
	r, err := GetRenderer(rendererName)
	if err != nil {
		out, _ := rendererNativePrompt.Render(env)
		return out, rendererNativePrompt.Name(), nil
	}
	out, rerr := r.Render(env)
	if rerr != nil {
		out, _ := rendererNativePrompt.Render(env)
		return out, rendererNativePrompt.Name(), nil
	}
	return out, r.Name(), nil
}

// compactJSONRenderer renders the envelope as compact (whitespace-free) JSON.
type compactJSONRenderer struct{}

func (compactJSONRenderer) Name() string { return "compact_json" }

func (compactJSONRenderer) Render(env *Envelope) (string, error) {
	b, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// humanRenderer renders a debugging-friendly human-readable envelope.
type humanRenderer struct{}

func (humanRenderer) Name() string { return "human" }

func (humanRenderer) Render(env *Envelope) (string, error) {
	return renderText(env, false), nil
}

// taggedRenderer renders a concise tagged-text packet.
type taggedRenderer struct{}

func (taggedRenderer) Name() string { return "tagged_text" }

func (taggedRenderer) Render(env *Envelope) (string, error) {
	return renderText(env, true), nil
}

// nativePromptRenderer converts LUI semantics back into a provider-neutral
// natural-language description. It is the safe default for models without
// verified LUI compatibility.
type nativePromptRenderer struct{}

func (nativePromptRenderer) Name() string { return "native_prompt" }

func (nativePromptRenderer) Render(env *Envelope) (string, error) {
	var b strings.Builder
	b.WriteString("Task: " + env.Task.Type + "\n")
	if env.Task.Summary != "" {
		b.WriteString("Summary: " + env.Task.Summary + "\n")
	}
	if len(env.Goals) > 0 {
		b.WriteString("Goals:\n")
		for _, g := range env.Goals {
			b.WriteString(" - " + g.Type + ": " + g.Summary + "\n")
		}
	}
	if len(env.Constraints) > 0 {
		b.WriteString("Constraints:\n")
		for _, c := range env.Constraints {
			b.WriteString(" - [" + string(c.Protection) + "] " + c.Type + ": " + c.Value + "\n")
		}
	}
	if len(env.State) > 0 {
		b.WriteString("State:\n")
		for _, s := range env.State {
			b.WriteString(" - " + s.Key + ": " + s.Value + "\n")
		}
	}
	if len(env.Tools) > 0 {
		b.WriteString("Tools:\n")
		for _, t := range env.Tools {
			b.WriteString(" - " + t.Name + "\n")
		}
	}
	return b.String(), nil
}
