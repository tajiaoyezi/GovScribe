package gateway

import (
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

type acAutomaton struct {
	nodes []acNode
}

type dictionaryTerm struct {
	text       string
	entityType EntityType
}

type acNode struct {
	next    map[byte]int
	fail    int
	outputs []dictionaryTerm
}

func newSimpleAutomaton(entries []dictionary.Entry) Automaton {
	automaton := &acAutomaton{nodes: []acNode{{next: make(map[byte]int)}}}
	for _, entry := range entries {
		if entry.Deleted || entry.Text == "" {
			continue
		}
		automaton.add(dictionaryTerm{
			text:       entry.Text,
			entityType: entityTypeFromDictionary(entry.Type),
		})
	}
	automaton.buildFailures()
	return automaton
}

func (a *acAutomaton) add(term dictionaryTerm) {
	state := 0
	for _, b := range []byte(term.text) {
		next, ok := a.nodes[state].next[b]
		if !ok {
			next = len(a.nodes)
			a.nodes = append(a.nodes, acNode{next: make(map[byte]int)})
			a.nodes[state].next[b] = next
		}
		state = next
	}
	a.nodes[state].outputs = append(a.nodes[state].outputs, term)
}

func (a *acAutomaton) buildFailures() {
	queue := make([]int, 0)
	for _, child := range a.nodes[0].next {
		a.nodes[child].fail = 0
		queue = append(queue, child)
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for b, child := range a.nodes[current].next {
			fail := a.nodes[current].fail
			for fail != 0 {
				if next, ok := a.nodes[fail].next[b]; ok {
					fail = next
					break
				}
				fail = a.nodes[fail].fail
			}
			if fail == 0 {
				if next, ok := a.nodes[0].next[b]; ok && next != child {
					fail = next
				}
			}
			a.nodes[child].fail = fail
			a.nodes[child].outputs = append(a.nodes[child].outputs, a.nodes[fail].outputs...)
			queue = append(queue, child)
		}
	}
}

func (a *acAutomaton) FindAll(text string) []Hit {
	var hits []Hit
	state := 0
	for i, b := range []byte(text) {
		for state != 0 {
			if _, ok := a.nodes[state].next[b]; ok {
				break
			}
			state = a.nodes[state].fail
		}
		if next, ok := a.nodes[state].next[b]; ok {
			state = next
		}
		for _, term := range a.nodes[state].outputs {
			end := i + 1
			start := end - len(term.text)
			hits = append(hits, Hit{
				Start:  start,
				End:    end,
				Text:   text[start:end],
				Type:   term.entityType,
				Source: SourceDictionary,
			})
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
