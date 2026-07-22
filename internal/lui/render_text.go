package lui

import (
	"fmt"
	"strings"
)

// renderText renders the envelope as either a human-readable or tagged-text
// representation.
func renderText(env *Envelope, tagged bool) string {
	var b strings.Builder
	if tagged {
		fmt.Fprintf(&b, "LUI/%s\n", env.Version)
		fmt.Fprintf(&b, "KIND %s\n", env.Kind)
	} else {
		fmt.Fprintf(&b, "LUI %s — %s\n", env.Version, env.Kind)
	}
	fmt.Fprintf(&b, "TASK %s\n", env.Task.Type)
	if env.Task.Complexity != "" {
		fmt.Fprintf(&b, "COMPLEXITY %s\n", env.Task.Complexity)
	}
	if env.Task.Summary != "" {
		fmt.Fprintf(&b, "SUMMARY %s\n", env.Task.Summary)
	}
	for _, g := range env.Goals {
		fmt.Fprintf(&b, "GOAL %s\n", g.Type)
	}
	for _, c := range env.Constraints {
		fmt.Fprintf(&b, "CONSTRAINT %s:%s\n", c.Protection, c.Value)
	}
	for _, s := range env.State {
		fmt.Fprintf(&b, "STATE %s=%s\n", s.Key, s.Value)
	}
	for _, t := range env.Tools {
		fmt.Fprintf(&b, "TOOL %s\n", t.Name)
	}
	for _, ref := range env.Context {
		body := ref.Content
		if body == "" && ref.URI != "" {
			body = ref.URI
		}
		fmt.Fprintf(&b, "CONTEXT %s\n", body)
	}
	for _, e := range env.Evidence {
		fmt.Fprintf(&b, "EVIDENCE %s\n", e.Summary)
	}
	if len(env.Output.Fields) > 0 {
		fmt.Fprintf(&b, "OUTPUT %s\n", strings.Join(env.Output.Fields, ","))
	}
	for k, v := range env.Dictionary {
		fmt.Fprintf(&b, "DICT %s=%s\n", k, v)
	}
	return b.String()
}
