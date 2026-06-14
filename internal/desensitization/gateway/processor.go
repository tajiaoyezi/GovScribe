package gateway

import "github.com/tajiaoyezi/GovScribe/internal/llm"

type TextProcessor interface {
	Sanitize(string) SanitizationResult
	SanitizeMessages([]llm.Message) ([]llm.Message, SanitizationResult)
}

type Processor struct {
	regex      RegexRecognizer
	dictionary *DictionaryRecognizer
}

func NewProcessor(dictionary *DictionaryRecognizer) Processor {
	return Processor{regex: NewRegexRecognizer(), dictionary: dictionary}
}

func (p Processor) Sanitize(text string) SanitizationResult {
	return ApplyPlaceholders(text, p.recognize(text))
}

func (p Processor) SanitizeMessages(messages []llm.Message) ([]llm.Message, SanitizationResult) {
	out := append([]llm.Message(nil), messages...)
	mapper := NewPlaceholderMapper()
	for i := range out {
		out[i].Content = mapper.Apply(out[i].Content, p.recognize(out[i].Content))
	}
	return out, SanitizationResult{Text: joinedMessages(out), Mappings: mapper.Mappings()}
}

func (p Processor) recognize(text string) []Hit {
	hits := p.regex.Recognize(text)
	if p.dictionary != nil {
		hits = append(hits, p.dictionary.Recognize(text)...)
	}
	return MergeHits(hits)
}
