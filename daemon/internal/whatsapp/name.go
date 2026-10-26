package whatsapp

import (
	"strings"
	"unicode"
)

// The longest names, in runes. A Phone Contact name and a document file name have no cap.
const (
	MaxPushName     = 25
	MaxGroupSubject = 100
)

// CleanName cleans a name that another person chose. It drops control, line, paragraph and format
// characters (Unicode Cc, Zl, Zp, Cf), trims white space and cuts the result to max runes (max <= 0: no cap).
// It keeps ZWJ and ZWNJ, which emoji and some scripts need, and the tag characters of subdivision flags.
func CleanName(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '‍', r == '‌', r >= 0xE0020 && r <= 0xE007F:
			return r
		case unicode.In(r, unicode.Cc, unicode.Zl, unicode.Zp, unicode.Cf):
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if max > 0 {
		if r := []rune(s); len(r) > max {
			s = strings.TrimSpace(string(r[:max]))
		}
	}
	return s
}
