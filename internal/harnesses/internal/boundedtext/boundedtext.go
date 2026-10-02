package boundedtext

import (
	"github.com/MartinAndreev/conduit/internal/sessions/provider"
	"unicode/utf8"
)

// String bounds text by bytes and preserves rune boundaries for valid UTF-8.
// OriginalBytes always reports the source byte length.
func String(value string, limit int) provider.BoundedText {
	original := uint64(len(value))
	if limit <= 0 {
		return provider.BoundedText{Truncated: len(value) > 0, OriginalBytes: provider.Some(original)}
	}
	if len(value) <= limit {
		return provider.BoundedText{Text: value, OriginalBytes: provider.Some(original)}
	}
	end := limit
	if utf8.ValidString(value) {
		for end > 0 && !utf8.RuneStart(value[end]) {
			end--
		}
	}
	return provider.BoundedText{Text: value[:end], Truncated: true, OriginalBytes: provider.Some(original)}
}

func Bytes(value []byte, limit int) provider.BoundedText { return String(string(value), limit) }
