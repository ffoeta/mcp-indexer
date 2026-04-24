package tokenize

import "strings"

// Stem применяет консервативный суффиксный стемминг.
// Только очевидные случаи: collectors→collector, collecting→collect.
func Stem(w string) string {
	if len(w) <= 3 {
		return w
	}
	for _, rule := range []struct{ suffix, replace string }{
		{"iers", "ier"},
		{"ies", "y"},
		{"ing", ""},
		{"ations", "ate"},
		{"ation", "ate"},
		{"ness", ""},
		{"ment", ""},
		{"ers", "er"},
		{"ed", ""},
		{"ly", ""},
		{"s", ""},
	} {
		if strings.HasSuffix(w, rule.suffix) {
			stem := w[:len(w)-len(rule.suffix)] + rule.replace
			if len(stem) > 2 {
				return stem
			}
		}
	}
	return w
}
