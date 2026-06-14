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

type PlaceholderMapper struct {
	byOriginal  map[string]Mapping
	countByType map[EntityType]int
	mappings    []Mapping
}

func NewPlaceholderMapper() *PlaceholderMapper {
	return &PlaceholderMapper{
		byOriginal:  make(map[string]Mapping),
		countByType: make(map[EntityType]int),
	}
}

func ApplyPlaceholders(text string, hits []Hit) SanitizationResult {
	mapper := NewPlaceholderMapper()
	sanitized := mapper.Apply(text, hits)
	return SanitizationResult{Text: sanitized, Mappings: mapper.Mappings()}
}

func (m *PlaceholderMapper) Apply(text string, hits []Hit) string {
	if len(hits) == 0 {
		return text
	}
	sorted := make([]Hit, len(hits))
	copy(sorted, hits)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })

	var builder strings.Builder
	cursor := 0

	for _, hit := range sorted {
		if hit.Start < cursor || hit.Start < 0 || hit.End > len(text) || hit.Start >= hit.End {
			continue
		}
		builder.WriteString(text[cursor:hit.Start])
		key := string(hit.Type) + "\x00" + hit.Text
		mapping, ok := m.byOriginal[key]
		if !ok {
			m.countByType[hit.Type]++
			mapping = Mapping{
				Placeholder: placeholderFor(hit.Type, m.countByType[hit.Type]),
				Original:    hit.Text,
				Type:        hit.Type,
				Source:      hit.Source,
			}
			m.byOriginal[key] = mapping
			m.mappings = append(m.mappings, mapping)
		}
		builder.WriteString(mapping.Placeholder)
		cursor = hit.End
	}
	builder.WriteString(text[cursor:])
	return builder.String()
}

func (m *PlaceholderMapper) Mappings() []Mapping {
	out := make([]Mapping, len(m.mappings))
	copy(out, m.mappings)
	return out
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
