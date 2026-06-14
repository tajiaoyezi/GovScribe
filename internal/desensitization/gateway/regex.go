package gateway

import "regexp"

type RegexRecognizer struct {
	patterns []regexPattern
}

type regexPattern struct {
	entityType EntityType
	re         *regexp.Regexp
}

func NewRegexRecognizer() RegexRecognizer {
	return RegexRecognizer{patterns: []regexPattern{
		{entityType: EntityTypeDocumentNumber, re: regexp.MustCompile(`(?:〔|\[)\d{4}(?:〕|\])[^\s，。；;、]{0,24}?号`)},
		{entityType: EntityTypeAmount, re: regexp.MustCompile(`(?:\d{1,3}(?:,\d{3})+|\d+)(?:\.\d+)?元|\d+(?:\.\d+)?万元`)},
		{entityType: EntityTypeIdentityNumber, re: regexp.MustCompile(`\d{17}[\dXx]`)},
		{entityType: EntityTypeUnifiedSocialCreditCode, re: regexp.MustCompile(`[0-9A-Z]{18}`)},
	}}
}

func (r RegexRecognizer) Recognize(text string) []Hit {
	var hits []Hit
	for _, pattern := range r.patterns {
		for _, span := range pattern.re.FindAllStringIndex(text, -1) {
			hits = append(hits, Hit{
				Start:  span[0],
				End:    span[1],
				Text:   text[span[0]:span[1]],
				Type:   pattern.entityType,
				Source: SourceRegex,
			})
		}
	}
	return hits
}
