package tokenize

import "strings"

// Normalizer токенизирует строку с учётом стоп-слов.
type Normalizer struct {
	stopWords map[string]struct{}
}

func New(stopWords map[string]struct{}) *Normalizer {
	return &Normalizer{stopWords: stopWords}
}

// Tokenize разбивает строку на нормализованные термины:
// delimiter-split → CamelCase-split → lowercase → stopwords → stem → дедупликация.
func (n *Normalizer) Tokenize(s string) []string {
	parts := SplitDelimiters(s)
	var raw []string
	for _, p := range parts {
		for _, sub := range SplitCamel(p) {
			raw = append(raw, sub)
		}
	}

	seen := make(map[string]struct{}, len(raw))
	result := make([]string, 0, len(raw))
	for _, tok := range raw {
		tok = strings.ToLower(strings.TrimSpace(tok))
		if len(tok) < 2 {
			continue
		}
		if _, stop := n.stopWords[tok]; stop {
			continue
		}
		tok = Stem(tok)
		if len(tok) < 2 {
			continue
		}
		if _, dup := seen[tok]; dup {
			continue
		}
		seen[tok] = struct{}{}
		result = append(result, tok)
	}
	return result
}
