package lui

import (
	"fmt"
	"strings"
)

const dictKeyPrefix = "d"
const dictKeyFmt = "d%04d"

// dictRefSyntax returns the unambiguous reference syntax for a dictionary key.
func dictRefSyntax(key string) string {
	return "{{lui:" + key + "}}"
}

// approxTokens is a conservative character-based token estimate used only for
// dictionary net-benefit decisions (not for authoritative accounting).
func approxTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

// DictionaryAllocator manages globally unique dictionary keys across an entire
// envelope. It deduplicates identical values and produces deterministic
// zero-padded 4-digit keys (d0001, d0002, …).
type DictionaryAllocator struct {
	next    int
	max     int
	entries map[string]string
	reverse map[string]string
}

// NewDictionaryAllocator creates an allocator that caps at maxEntries.
func NewDictionaryAllocator(maxEntries int) *DictionaryAllocator {
	return &DictionaryAllocator{
		next:    1,
		max:     maxEntries,
		entries: make(map[string]string),
		reverse: make(map[string]string),
	}
}

// Intern returns the key for value, allocating a new zero-padded key if value
// has not been seen before. It returns "" when the maximum entry count has been
// reached.
func (a *DictionaryAllocator) Intern(value string) string {
	if key, ok := a.reverse[value]; ok {
		return key
	}
	if a.next > a.max {
		return ""
	}
	key := fmt.Sprintf(dictKeyFmt, a.next)
	a.next++
	a.entries[key] = value
	a.reverse[value] = key
	return key
}

// Snapshot returns a copy of the allocated key → value entries.
func (a *DictionaryAllocator) Snapshot() map[string]string {
	cp := make(map[string]string, len(a.entries))
	for k, v := range a.entries {
		cp[k] = v
	}
	return cp
}

// BuildDictionary compresses repeated long substrings in text into a dictionary
// of stable identifiers. It applies the net-benefit rule: an entry is only kept
// when the repeated-original tokens exceed the dictionary definition plus
// reference tokens. Returns the dictionary, the compressed text, and the
// estimated token savings.
func BuildDictionary(text string, minLen, maxEntries int) (map[string]string, string, int) {
	dict := map[string]string{}
	compressed := text
	saved := 0
	keyN := 0
	for {
		if len(dict) >= maxEntries {
			break
		}
		best, bestSaving := "", 0
		for start := 0; start+minLen <= len(compressed); start++ {
			for end := start + minLen; end <= len(compressed) && end-start <= 256; end++ {
				sub := compressed[start:end]
				if strings.Count(compressed, sub) < 2 {
					continue
				}
				count := strings.Count(compressed, sub)
				defTokens := approxTokens(sub)
				refTokens := approxTokens(dictRefSyntax(dictKeyPrefix + "N"))
				gain := count*defTokens - defTokens - count*refTokens
				if gain > bestSaving {
					best, bestSaving = sub, gain
				}
			}
		}
		if best == "" || bestSaving <= 0 {
			break
		}
		keyN++
		key := dictKeyPrefix + itoa(keyN)
		dict[key] = best
		compressed = strings.ReplaceAll(compressed, best, dictRefSyntax(key))
		saved += bestSaving
	}
	return dict, compressed, saved
}

// compressTextWithAllocator compresses text using the given DictionaryAllocator,
// which ensures globally unique keys across all fields in an envelope.
func compressTextWithAllocator(text string, minLen int, alloc *DictionaryAllocator) (string, int) {
	compressed := text
	saved := 0
	for {
		best, bestSaving := "", 0
		for start := 0; start+minLen <= len(compressed); start++ {
			for end := start + minLen; end <= len(compressed) && end-start <= 256; end++ {
				sub := compressed[start:end]
				if strings.Count(compressed, sub) < 2 {
					continue
				}
				count := strings.Count(compressed, sub)
				defTokens := approxTokens(sub)
				refTokens := approxTokens(dictRefSyntax("d0000"))
				gain := count*defTokens - defTokens - count*refTokens
				if gain > bestSaving {
					best, bestSaving = sub, gain
				}
			}
		}
		if best == "" || bestSaving <= 0 {
			break
		}
		key := alloc.Intern(best)
		if key == "" {
			break
		}
		compressed = strings.ReplaceAll(compressed, best, dictRefSyntax(key))
		saved += bestSaving
	}
	return compressed, saved
}

// ExpandDictionary reverses BuildDictionary, replacing dictionary references
// with their definitions. Unknown keys are left untouched.
//
// Safety: multi-pass expansion (up to 10 levels), 10x size limit enforced
// by predicting the size before each replacement, and self-reference cycle
// detection. Returns an error if the expansion would exceed the size limit.
func ExpandDictionary(text string, dict map[string]string) (string, error) {
	keys := make([]string, 0, len(dict))
	for k := range dict {
		keys = append(keys, k)
	}
	// Sort keys by reference length descending so longer references are
	// expanded before shorter ones (prevents partial expansion).
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && len(dictRefSyntax(keys[j])) > len(dictRefSyntax(keys[j-1])); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}

	maxDepth := 10
	maxSize := len(text) * 10
	if maxSize < 1024 {
		maxSize = 1024
	}

	result := text
	for depth := 0; depth < maxDepth; depth++ {
		if len(result) > maxSize {
			return "", fmt.Errorf("lui_dictionary_expansion_limit: expanded size exceeds %d bytes", maxSize)
		}
		prev := result
		for _, k := range keys {
			ref := dictRefSyntax(k)
			// Skip self-referencing values (cycle detection).
			if strings.Contains(dict[k], ref) {
				continue
			}
			// Predict the size after replacement to avoid exceeding the limit
			// with a single ReplaceAll that could blow past it.
			count := strings.Count(result, ref)
			if count > 0 {
				refLen := len(ref)
				valLen := len(dict[k])
				predicted := len(result) + count*(valLen-refLen)
				if predicted > maxSize {
					continue
				}
			}
			result = strings.ReplaceAll(result, ref, dict[k])
		}
		if result == prev {
			break
		}
	}
	return result, nil
}

// CompressEnvelopeText applies dictionary compression to the envelope's text
// fields (constraint values, context content, state values) and stores the
// resulting dictionary on the envelope. It returns the estimated token savings
// and a flag indicating whether any compression occurred.
//
// Uses a single DictionaryAllocator across all fields so keys are globally
// unique and identical values are safely deduplicated.
func CompressEnvelopeText(env *Envelope, minLen, maxEntries int) (int, bool) {
	if env == nil {
		return 0, false
	}
	alloc := NewDictionaryAllocator(maxEntries)
	totalSaved := 0
	changed := false

	for i := range env.Constraints {
		item := &env.Constraints[i]
		if item.Protection == ProtectionImmutable || item.Protection == ProtectionProtected {
			continue
		}
		comp, saved := compressTextWithAllocator(item.Value, minLen, alloc)
		if saved > 0 {
			item.Value = comp
			totalSaved += saved
			changed = true
		}
	}
	for i := range env.State {
		item := &env.State[i]
		if item.Protection == ProtectionImmutable || item.Protection == ProtectionProtected {
			continue
		}
		comp, saved := compressTextWithAllocator(item.Value, minLen, alloc)
		if saved > 0 {
			item.Value = comp
			totalSaved += saved
			changed = true
		}
	}
	for i := range env.Context {
		item := &env.Context[i]
		if !item.Inline || item.Protection == ProtectionImmutable || item.Protection == ProtectionProtected {
			continue
		}
		comp, saved := compressTextWithAllocator(item.Content, minLen, alloc)
		if saved > 0 {
			item.Content = comp
			totalSaved += saved
			changed = true
		}
	}

	if changed {
		if env.Dictionary == nil {
			env.Dictionary = map[string]string{}
		}
		for k, v := range alloc.Snapshot() {
			if _, ok := env.Dictionary[k]; !ok {
				env.Dictionary[k] = v
			}
		}
	}
	return totalSaved, changed
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
