package external

import (
	"context"
	"strings"
)

// summarizeEvidence uses the LLM summarizer to convert fetched search results
// into structured evidence records. The searcher (if it implements PageFetcher)
// enriches results with page text; otherwise the snippet text is used.
func summarizeEvidence(ctx context.Context, sum Summarizer, srch Searcher, id ModelIdentity, results []SearchResult) []EvidenceRecord {
	pages := resultsToPages(srch, results)
	if len(pages) == 0 {
		return nil
	}
	summary, err := sum.SummarizeEvidence(ctx, id.Name, pages)
	if err != nil {
		return nil
	}
	var recs []EvidenceRecord
	seen := map[string]bool{}
	for _, c := range summary.Capabilities {
		if c.Score <= 0 {
			continue
		}
		dkey := string(c.Capability) + "|" + c.Evidence
		if seen[dkey] {
			continue
		}
		seen[dkey] = true
		src := SourceAAII
		if c.Evidence != "" {
			low := strings.ToLower(c.Evidence)
			switch {
			case strings.Contains(low, "livebench"):
				src = SourceLiveBench
			case strings.Contains(low, "swe") || strings.Contains(low, "swebench"):
				src = SourceSWEBench
			case strings.Contains(low, "arena"):
				src = SourceLMArena
			}
		}
		// The published identity must be established from what the evidence
		// actually reports (summary.Model), not copied from the configured
		// identity. Copying would make variant matching (§18) always report
		// "exact" and silently credit a preview/thinking variant to the base
		// model. parsePublishedIdentity returns a zero identity (treated as
		// exact by Match) only when the source gave no model name.
		published := parsePublishedIdentity(summary.Model)
		recs = append(recs, EvidenceRecord{
			Source:        src,
			ModelIdentity: id.ID,
			Published:     published,
			Benchmark:     "llm-summary/" + string(c.Capability),
			Value:         c.Score * 10.0, // store as 0-100 for consistent normalization
			Scale:         ScaleZeroToHundred,
			Capability:    c.Capability,
			ReportedAt:    registryUpdatedAt,
			URL:           c.Evidence,
			Notes:         c.Note,
		})
	}
	return recs
}

// resultsToPages extracts benchmark-relevant page text from search results.
// If the searcher implements PageFetcher, it fetches full page text; otherwise
// the search snippet is used directly.
func resultsToPages(srch Searcher, results []SearchResult) []PageText {
	var pages []PageText
	var fetcher PageFetcher
	if f, ok := srch.(PageFetcher); ok {
		fetcher = f
	}
	for _, r := range results {
		text := strings.TrimSpace(r.Snippet)
		if fetcher != nil && r.URL != "" {
			if pt, err := fetcher.FetchPage(r.URL); err == nil && pt != "" {
				text = pt
			}
		}
		if text == "" {
			continue
		}
		pages = append(pages, PageText{URL: r.URL, Title: r.Title, Text: text})
	}
	return pages
}

// PageFetcher is an optional interface a Searcher may implement to return the
// benchmark-relevant text of a page by URL.
type PageFetcher interface {
	FetchPage(url string) (string, error)
}
