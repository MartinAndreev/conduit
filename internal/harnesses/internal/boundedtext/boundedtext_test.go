package boundedtext

import (
	"testing"
	"unicode/utf8"
)

func TestStringBoundsBytesAndPreservesUTF8(t *testing.T) {
	got := String("a€b", 3)
	if got.Text != "a" || !utf8.ValidString(got.Text) || !got.Truncated || !got.OriginalBytes.Present || got.OriginalBytes.Value != 5 {
		t.Fatalf("unexpected bound: %#v", got)
	}
	exact := String("a€", 4)
	if exact.Text != "a€" || exact.Truncated || exact.OriginalBytes.Value != 4 {
		t.Fatalf("unexpected exact bound: %#v", exact)
	}
}

func TestStringNonpositiveLimit(t *testing.T) {
	for _, limit := range []int{0, -1} {
		got := String("abc", limit)
		if got.Text != "" || !got.Truncated || !got.OriginalBytes.Present || got.OriginalBytes.Value != 3 {
			t.Fatalf("limit %d: %#v", limit, got)
		}
	}
	empty := String("", 0)
	if empty.Truncated || !empty.OriginalBytes.Present || empty.OriginalBytes.Value != 0 {
		t.Fatalf("empty: %#v", empty)
	}
}
