package gateway

type TextProcessor interface {
	Sanitize(string) SanitizationResult
}

type Processor struct {
	regex      RegexRecognizer
	dictionary *DictionaryRecognizer
}

func NewProcessor(dictionary *DictionaryRecognizer) Processor {
	return Processor{regex: NewRegexRecognizer(), dictionary: dictionary}
}

func (p Processor) Sanitize(text string) SanitizationResult {
	hits := p.regex.Recognize(text)
	if p.dictionary != nil {
		hits = append(hits, p.dictionary.Recognize(text)...)
	}
	return ApplyPlaceholders(text, MergeHits(hits))
}
