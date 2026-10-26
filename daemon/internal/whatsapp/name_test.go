package whatsapp

import (
	"strings"
	"testing"
)

func TestCleanName(t *testing.T) {
	keep := []string{"\U0001F468\u200D\U0001F469\u200D\U0001F467", "\u0645\u06CC\u200C\u062E\u0648\u0627\u0647\u0645", "\U0001F3F4\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F"}
	for _, s := range keep {
		if got := CleanName(s, 0); got != s {
			t.Errorf("CleanName(%q) = %q, want it unchanged", s, got)
		}
	}
	for in, want := range map[string]string{
		"Mom\n\n\n": "Mom",
		"a\u202eb\u2066c\u200bd\ufeffe\u2028f\u061cg": "abcdefg",
		"\x00\x07\n\t": "",
		"  Rob  ":      "Rob",
	} {
		if got := CleanName(in, 0); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := CleanName(strings.Repeat("é", 40), MaxPushName); got != strings.Repeat("é", 25) {
		t.Errorf("push name cap: got %d runes", len([]rune(got)))
	}
	if got := CleanName(strings.Repeat("é", 150), MaxGroupSubject); len([]rune(got)) != 100 {
		t.Errorf("subject cap: got %d runes", len([]rune(got)))
	}
}
