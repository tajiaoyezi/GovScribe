package gateway

import (
	"context"
	"sync/atomic"

	ahocorasick "github.com/petar-dambovaliev/aho-corasick"
	"github.com/tajiaoyezi/GovScribe/internal/desensitization/dictionary"
)

type Automaton interface {
	FindAll(string) []Hit
}

type AutomatonBuilder func([]dictionary.Entry) Automaton

type DictionaryRecognizer struct {
	automaton atomic.Value
	builder   AutomatonBuilder
}

func NewDictionaryRecognizer(entries []dictionary.Entry) *DictionaryRecognizer {
	return NewDictionaryRecognizerWithBuilder(entries, newACAutomaton)
}

func NewDictionaryRecognizerWithBuilder(entries []dictionary.Entry, builder AutomatonBuilder) *DictionaryRecognizer {
	if builder == nil {
		builder = newACAutomaton
	}
	r := &DictionaryRecognizer{builder: builder}
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
	builder := r.builder
	if builder == nil {
		builder = newACAutomaton
	}
	r.automaton.Store(builder(entries))
}

type DictionaryRecognizerReloader struct {
	Recognizer *DictionaryRecognizer
}

func NewDictionaryRecognizerReloader(recognizer *DictionaryRecognizer) DictionaryRecognizerReloader {
	return DictionaryRecognizerReloader{Recognizer: recognizer}
}

func (r DictionaryRecognizerReloader) ReloadDictionary(_ context.Context, entries []dictionary.Entry) error {
	if r.Recognizer != nil {
		r.Recognizer.SwapEntries(entries)
	}
	return nil
}

type dictionaryTerm struct {
	text       string
	entityType EntityType
}

type petarAutomaton struct {
	matcher ahocorasick.AhoCorasick
	terms   []dictionaryTerm
}

func newACAutomaton(entries []dictionary.Entry) Automaton {
	terms := make([]dictionaryTerm, 0, len(entries))
	for _, entry := range entries {
		if entry.Deleted || entry.Text == "" {
			continue
		}
		terms = append(terms, dictionaryTerm{
			text:       entry.Text,
			entityType: entityTypeFromDictionary(entry.Type),
		})
	}
	if len(terms) == 0 {
		return &petarAutomaton{}
	}
	patterns := make([]string, len(terms))
	for i, term := range terms {
		patterns[i] = term.text
	}
	builder := ahocorasick.NewAhoCorasickBuilder(ahocorasick.Opts{
		MatchKind: ahocorasick.StandardMatch,
	})
	return &petarAutomaton{matcher: builder.Build(patterns), terms: terms}
}

func (a *petarAutomaton) FindAll(text string) []Hit {
	if len(a.terms) == 0 {
		return nil
	}
	var hits []Hit
	iter := a.matcher.IterOverlapping(text)
	for match := iter.Next(); match != nil; match = iter.Next() {
		pattern := match.Pattern()
		if pattern < 0 || pattern >= len(a.terms) {
			continue
		}
		start, end := match.Start(), match.End()
		if start < 0 || end > len(text) || start >= end {
			continue
		}
		term := a.terms[pattern]
		hits = append(hits, Hit{
			Start:  start,
			End:    end,
			Text:   text[start:end],
			Type:   term.entityType,
			Source: SourceDictionary,
		})
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
