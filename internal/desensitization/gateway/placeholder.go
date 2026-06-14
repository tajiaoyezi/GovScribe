package gateway

import (
	"fmt"
	"sort"
	"strings"
)

type Mapping struct {
	Placeholder string
	Original    string
	Type        EntityType
	Source      Source
}

type SanitizationResult struct {
	Text     string
	Mappings []Mapping
}

func ApplyPlaceholders(text string, hits []Hit) SanitizationResult {
	if len(hits) == 0 {
		return SanitizationResult{Text: text}
	}
	sorted := make([]Hit, len(hits))
	copy(sorted, hits)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })

	var builder strings.Builder
	cursor := 0
	byOriginal := map[string]Mapping{}
	countByType := map[EntityType]int{}
	mappings := make([]Mapping, 0, len(sorted))

	for _, hit := range sorted {
		if hit.Start < cursor || hit.Start < 0 || hit.End > len(text) || hit.Start >= hit.End {
			continue
		}
		builder.WriteString(text[cursor:hit.Start])
		key := string(hit.Type) + "\x00" + hit.Text
		mapping, ok := byOriginal[key]
		if !ok {
			countByType[hit.Type]++
			mapping = Mapping{
				Placeholder: placeholderFor(hit.Type, countByType[hit.Type]),
				Original:    hit.Text,
				Type:        hit.Type,
				Source:      hit.Source,
			}
			byOriginal[key] = mapping
			mappings = append(mappings, mapping)
		}
		builder.WriteString(mapping.Placeholder)
		cursor = hit.End
	}
	builder.WriteString(text[cursor:])
	return SanitizationResult{Text: builder.String(), Mappings: mappings}
}

func (r SanitizationResult) Restore(text string) string {
	restored := text
	for _, mapping := range r.Mappings {
		restored = strings.ReplaceAll(restored, mapping.Placeholder, mapping.Original)
	}
	return restored
}

func placeholderFor(entityType EntityType, sequence int) string {
	return fmt.Sprintf("〖%s_%02d〗", placeholderType(entityType), sequence)
}

func placeholderType(entityType EntityType) string {
	return strings.ToUpper(string(entityType))
}
