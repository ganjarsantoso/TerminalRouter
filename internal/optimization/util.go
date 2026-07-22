package optimization

import (
	"encoding/json"
	"regexp"
)

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

var locationRE = regexp.MustCompile(`[A-Za-z0-9_./-]+\.[A-Za-z0-9]{1,6}:\d{1,7}`)

// extractLocations returns candidate protected substrings (file:line locations
// and diff hunks) found in text so compactors preserve them verbatim.
func extractLocations(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	for _, m := range locationRE.FindAllString(text, -1) {
		out = append(out, m)
	}
	return out
}
