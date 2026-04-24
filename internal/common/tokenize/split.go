package tokenize

import (
	"strings"
	"unicode"
)

// SplitCamel разбивает camelCase/PascalCase: "getUserName" -> ["get","User","Name"].
func SplitCamel(s string) []string {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	var parts []string
	start := 0
	for i := 1; i < len(runes); i++ {
		if unicode.IsUpper(runes[i]) &&
			(unicode.IsLower(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
			if i > start {
				parts = append(parts, string(runes[start:i]))
			}
			start = i
		}
	}
	parts = append(parts, string(runes[start:]))
	return parts
}

// SplitDelimiters разбивает по /._-: и пробелам.
func SplitDelimiters(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == '/' || r == '.' || r == '_' || r == '-' || r == ':' || r == ' ' || r == '\t'
	})
}
