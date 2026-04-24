package tokenize

// DefaultStopSet возвращает стоп-слова по умолчанию.
func DefaultStopSet() map[string]struct{} {
	words := []string{
		"a", "an", "the", "and", "or", "not", "in", "is", "it", "of", "to",
		"as", "at", "be", "by", "do", "for", "if", "on", "up", "we",
		"self", "this", "super", "true", "false", "null", "nil", "none",
		"new", "return", "import", "from", "def", "class", "func", "var",
		"let", "const", "type", "struct", "interface", "public", "private",
		"protected", "static", "final", "void", "int", "str", "bool",
		"list", "map", "set", "get", "err", "error",
	}
	m := make(map[string]struct{}, len(words))
	for _, w := range words {
		m[w] = struct{}{}
	}
	return m
}

// BuildStopSet строит set из переданного списка (из config).
func BuildStopSet(words []string) map[string]struct{} {
	m := make(map[string]struct{}, len(words))
	for _, w := range words {
		m[w] = struct{}{}
	}
	return m
}
