package gateway

import (
	"context"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

type TextProcessor interface {
	Sanitize(string) SanitizationResult
	SanitizeMessages([]llm.Message) ([]llm.Message, SanitizationResult)
}

type ContextTextProcessor interface {
	SanitizeMessagesContext(context.Context, []llm.Message) ([]llm.Message, SanitizationResult, error)
}

type Processor struct {
	regex      RegexRecognizer
	dictionary *DictionaryRecognizer
	ner        NERClient
}

func NewProcessor(dictionary *DictionaryRecognizer) Processor {
	return Processor{regex: NewRegexRecognizer(), dictionary: dictionary}
}

func NewProcessorWithNER(dictionary *DictionaryRecognizer, ner NERClient) Processor {
	return Processor{regex: NewRegexRecognizer(), dictionary: dictionary, ner: ner}
}

func (p Processor) Sanitize(text string) SanitizationResult {
	return ApplyPlaceholders(text, p.recognize(text))
}

func (p Processor) SanitizeMessages(messages []llm.Message) ([]llm.Message, SanitizationResult) {
	out := append([]llm.Message(nil), messages...)
	mapper := NewPlaceholderMapper()
	diffs := make([]MessageDiff, 0, len(out))
	for i := range out {
		before := out[i].Content
		out[i].Content = mapper.ApplyWithMessageContext(out[i].Content, p.recognize(out[i].Content), i, out[i].Role)
		diffs = append(diffs, MessageDiff{
			MessageIndex: i,
			Role:         out[i].Role,
			Before:       before,
			After:        out[i].Content,
			Changed:      before != out[i].Content,
		})
	}
	return out, SanitizationResult{Text: joinedMessages(out), Mappings: mapper.Mappings(), Matches: mapper.Matches(), MessageDiffs: diffs}
}

func (p Processor) SanitizeMessagesContext(ctx context.Context, messages []llm.Message) ([]llm.Message, SanitizationResult, error) {
	out := append([]llm.Message(nil), messages...)
	mapper := NewPlaceholderMapper()
	diffs := make([]MessageDiff, 0, len(out))
	for i := range out {
		before := out[i].Content
		hits, err := p.recognizeWithNER(ctx, out[i].Content)
		if err != nil {
			return nil, SanitizationResult{}, err
		}
		out[i].Content = mapper.ApplyWithMessageContext(out[i].Content, hits, i, out[i].Role)
		diffs = append(diffs, MessageDiff{
			MessageIndex: i,
			Role:         out[i].Role,
			Before:       before,
			After:        out[i].Content,
			Changed:      before != out[i].Content,
		})
	}
	return out, SanitizationResult{Text: joinedMessages(out), Mappings: mapper.Mappings(), Matches: mapper.Matches(), MessageDiffs: diffs}, nil
}

func (p Processor) recognize(text string) []Hit {
	hits := p.regex.Recognize(text)
	if p.dictionary != nil {
		hits = append(hits, p.dictionary.Recognize(text)...)
	}
	return MergeHits(hits)
}

func (p Processor) recognizeWithNER(ctx context.Context, text string) ([]Hit, error) {
	hits := p.recognize(text)
	if p.ner == nil {
		return hits, nil
	}
	nerHits, err := p.ner.Recognize(ctx, text)
	if err != nil {
		return nil, err
	}
	hits = append(hits, nerHits...)
	return MergeHits(hits), nil
}
