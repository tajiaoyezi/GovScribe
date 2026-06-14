package gateway

import "sort"

func MergeHits(hits []Hit) []Hit {
	if len(hits) == 0 {
		return nil
	}
	sorted := make([]Hit, len(hits))
	copy(sorted, hits)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Start != sorted[j].Start {
			return sorted[i].Start < sorted[j].Start
		}
		if priority(sorted[i]) != priority(sorted[j]) {
			return priority(sorted[i]) > priority(sorted[j])
		}
		return sorted[i].length() > sorted[j].length()
	})

	merged := make([]Hit, 0, len(sorted))
	for _, hit := range sorted {
		if hit.Start >= hit.End {
			continue
		}
		overlapIndex := -1
		for i, existing := range merged {
			if hit.overlaps(existing) {
				overlapIndex = i
				break
			}
		}
		if overlapIndex == -1 {
			merged = append(merged, hit)
			continue
		}
		if betterHit(hit, merged[overlapIndex]) {
			merged[overlapIndex] = hit
		}
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Start < merged[j].Start })
	return merged
}

func betterHit(candidate, existing Hit) bool {
	if priority(candidate) != priority(existing) {
		return priority(candidate) > priority(existing)
	}
	return candidate.length() > existing.length()
}

func priority(hit Hit) int {
	if hit.Type == EntityTypeSecretKeywordBlacklist {
		return 4
	}
	switch hit.Source {
	case SourceRegex:
		return 3
	case SourceDictionary:
		return 2
	case SourceNER:
		return 1
	default:
		return 0
	}
}
