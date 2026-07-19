package external

import "math"

// normalize maps a native-scale value to the universal 0-10 scale per source.
// It returns the normalized score and whether the source is recognized.
func normalize(rec EvidenceRecord) NormalizedScore {
	meta, ok := sourceMetaByID(rec.Source)
	tier := TrustModerate
	if ok {
		tier = meta.TrustTier
	}
	n := NormalizedScore{
		Source:   rec.Source,
		Raw:      rec.Value,
		RawScale: rec.Scale,
		Tier:     tier,
	}
	switch rec.Scale {
	case ScaleZeroToHundred:
		n.Normalized = clamp10(rec.Value / 10.0)
	case ScaleZeroToOne:
		n.Normalized = clamp10(rec.Value * 10.0)
	case ScaleZeroToTen:
		n.Normalized = clamp10(rec.Value)
	case ScaleElo:
		// Elo is normalized relative to a fixed arena median reference.
		// 1270 Elo ~= 7.0 on the universal scale; scale 100 Elo ~= 2.0 points.
		median := 1270.0
		n.Normalized = clamp10(7.0 + (rec.Value-median)/100.0*2.0)
	default:
		n.Normalized = clamp10(rec.Value)
	}
	return n
}

func clamp10(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 10 {
		return 10
	}
	return v
}

// tierWeight returns a trust weight used by the consensus aggregation.
func tierWeight(t TrustTier) float64 {
	switch t {
	case TrustHigh:
		return 1.0
	case TrustModerate:
		return 0.7
	case TrustLow:
		return 0.4
	default:
		return 0.7
	}
}

// roundHalf rounds to the nearest 0.5 increment (matching the router scale).
func roundHalf(v float64) float64 {
	return math.Round(v*2) / 2
}
