package gateway

import "strings"

const (
	placeholderStart = "〖"
	placeholderEnd   = "〗"
)

type placeholderTailBuffer struct {
	result       SanitizationResult
	tail         string
	maxTailBytes int
}

func newPlaceholderTailBuffer(result SanitizationResult) *placeholderTailBuffer {
	maxTailBytes := 0
	for _, mapping := range result.Mappings {
		if len(mapping.Placeholder) > maxTailBytes {
			maxTailBytes = len(mapping.Placeholder)
		}
	}
	return &placeholderTailBuffer{result: result, maxTailBytes: maxTailBytes}
}

func (b *placeholderTailBuffer) Push(delta string) string {
	if delta == "" {
		return ""
	}
	combined := b.tail + delta
	emit, tail := splitPlaceholderTail(combined, b.maxTailBytes)
	b.tail = tail
	return b.result.Restore(emit)
}

func (b *placeholderTailBuffer) Flush() string {
	if b.tail == "" {
		return ""
	}
	out := b.result.Restore(b.tail)
	b.tail = ""
	return out
}

func splitPlaceholderTail(text string, maxTailBytes int) (string, string) {
	start := strings.LastIndex(text, placeholderStart)
	if start == -1 {
		return text, ""
	}
	endAfterStart := strings.Index(text[start:], placeholderEnd)
	if endAfterStart == -1 {
		if maxTailBytes == 0 || len(text[start:]) > maxTailBytes {
			return text, ""
		}
		return text[:start], text[start:]
	}
	return text, ""
}
