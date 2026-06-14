package gateway

import (
	"strings"
	"sync/atomic"

	"github.com/tajiaoyezi/GovScribe/internal/desensitization/dictionary"
)

type Automaton interface {
	FindAll(string) []Hit
}

type DictionaryRecognizer struct {
	automaton atomic.Value
}

func NewDictionaryRecognizer(entries []dictionary.Entry) *DictionaryRecognizer {
	r := &DictionaryRecognizer{}
	r.SwapEntries(entries)
	return r
}

func (r *DictionaryRecognizer) Recognize(text string) []Hit {
	automaton, ok := r.automaton.Load().(Automaton)
	if !ok || automaton == nil {
		return nil
	}
	return automaton.FindAll(text)
}

func (r *DictionaryRecognizer) SwapEntries(entries []dictionary.Entry) {
	r.automaton.Store(newSimpleAutomaton(entries))
}

type simpleAutomaton struct {
	terms []dictionaryTerm
}

type dictionaryTerm struct {
	text       string
	entityType EntityType
}

func newSimpleAutomaton(entries []dictionary.Entry) Automaton {
	terms := make([]dictionaryTerm, 0, len(entries))
	for _, entry := range entries {
		if entry.Deleted || strings.TrimSpace(entry.Text) == "" {
			continue
		}
		terms = append(terms, dictionaryTerm{
			text:       entry.Text,
			entityType: entityTypeFromDictionary(entry.Type),
		})
	}
	return simpleAutomaton{terms: terms}
}

func (a simpleAutomaton) FindAll(text string) []Hit {
	var hits []Hit
	for _, term := range a.terms {
		start := 0
		for {
			index := strings.Index(text[start:], term.text)
			if index < 0 {
				break
			}
			hitStart := start + index
			hitEnd := hitStart + len(term.text)
			hits = append(hits, Hit{
				Start:  hitStart,
				End:    hitEnd,
				Text:   text[hitStart:hitEnd],
				Type:   term.entityType,
				Source: SourceDictionary,
			})
			start = hitEnd
		}
	}
	return hits
}

func entityTypeFromDictionary(entryType dictionary.EntryType) EntityType {
	switch entryType {
	case dictionary.EntryTypeOrganization:
		return EntityTypeOrganization
	case dictionary.EntryTypePerson:
		return EntityTypePerson
	case dictionary.EntryTypeProjectCode:
		return EntityTypeProjectCode
	case dictionary.EntryTypeSecretKeywordBlacklist:
		return EntityTypeSecretKeywordBlacklist
	default:
		return EntityTypeNamedEntity
	}
}
