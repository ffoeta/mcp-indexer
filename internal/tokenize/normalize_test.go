package tokenize

import (
	"testing"
)

func makeNormalizer(stopwords ...string) *Normalizer {
	sw := map[string]struct{}{}
	for _, w := range stopwords {
		sw[w] = struct{}{}
	}
	return New(sw)
}

// K1: Tokenize_CamelCase_Split
func TestTokenize_CamelCase_Split(t *testing.T) {
	n := makeNormalizer()
	tokens := n.Tokenize("getUserName")
	found := map[string]bool{}
	for _, t := range tokens {
		found[t] = true
	}
	// "get", "user", "name" — after lowercase and dedup
	if !found["user"] || !found["name"] {
		t.Errorf("expected user+name in %v", tokens)
	}
}

// K2: Tokenize_SnakeCase_Split
func TestTokenize_SnakeCase_Split(t *testing.T) {
	n := makeNormalizer()
	tokens := n.Tokenize("user_name_service")
	found := map[string]bool{}
	for _, t := range tokens {
		found[t] = true
	}
	if !found["user"] || !found["name"] {
		t.Errorf("expected user+name in %v", tokens)
	}
}

// K3: Tokenize_Stopwords_Filtered
func TestTokenize_Stopwords_Filtered(t *testing.T) {
	n := makeNormalizer("get", "the", "is")
	tokens := n.Tokenize("getUser")
	for _, tok := range tokens {
		if tok == "get" {
			t.Error("stopword 'get' should be filtered")
		}
	}
}

// K4: Tokenize_Lowercase
func TestTokenize_Lowercase(t *testing.T) {
	n := makeNormalizer()
	tokens := n.Tokenize("UserService")
	for _, tok := range tokens {
		if tok != toLower(tok) {
			t.Errorf("token %q not lowercase", tok)
		}
	}
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			result[i] = byte(c + 32)
		} else {
			result[i] = byte(c)
		}
	}
	return string(result)
}

// K5: Tokenize_Deduplicates
func TestTokenize_Deduplicates(t *testing.T) {
	n := makeNormalizer()
	tokens := n.Tokenize("foo_foo")
	count := 0
	for _, tok := range tokens {
		if tok == "foo" {
			count++
		}
	}
	if count > 1 {
		t.Errorf("duplicate token 'foo': count=%d", count)
	}
}

// K6: Tokenize_ShortTokens_Skipped
func TestTokenize_ShortTokens_Skipped(t *testing.T) {
	n := makeNormalizer()
	tokens := n.Tokenize("a_bb_ccc")
	for _, tok := range tokens {
		if len(tok) < 2 {
			t.Errorf("token %q too short, should be filtered", tok)
		}
	}
}

// K7: Stem_Collectors_To_Collector
func TestStem_Collectors(t *testing.T) {
	// Verify that Stem strips common suffixes.
	// "collectors" → "collector" (ers→er)
	// "collecting" → "collect" (ing→"")
	// "collected"  → "collect" (ed→"")
	// "services"   → "service" (s→"")
	// "running"    → "runn"    (ing→""; 4 chars is > 2 so kept)
	cases := []struct{ in, want string }{
		{"collectors", "collector"},
		{"collecting", "collect"},
		{"collected", "collect"},
		{"services", "service"},
		{"running", "runn"},
	}
	for _, c := range cases {
		got := Stem(c.in)
		if got != c.want {
			t.Errorf("Stem(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// K8: SplitCamel_PascalCase
func TestSplitCamel_PascalCase(t *testing.T) {
	parts := SplitCamel("UserService")
	if len(parts) < 2 {
		t.Errorf("expected 2+ parts from PascalCase, got %v", parts)
	}
}

// K9: SplitDelimiters_PathSegments
func TestSplitDelimiters_PathSegments(t *testing.T) {
	parts := SplitDelimiters("src/pkg/user_service.py")
	// expect: src, pkg, user_service, py — but _ is also delimiter
	// so: src, pkg, user, service, py
	if len(parts) < 3 {
		t.Errorf("expected ≥3 parts from path, got %v", parts)
	}
}

// K10: Tokenize_EmptyString_ReturnsEmpty
func TestTokenize_EmptyString_ReturnsEmpty(t *testing.T) {
	n := makeNormalizer()
	tokens := n.Tokenize("")
	if len(tokens) != 0 {
		t.Errorf("expected empty tokens for empty input, got %v", tokens)
	}
}

// K11: Tokenize_PathWithPrefix_Works
func TestTokenize_PathWithPrefix_Works(t *testing.T) {
	n := makeNormalizer()
	tokens := n.Tokenize("src:pkg/model.py")
	if len(tokens) == 0 {
		t.Error("expected tokens for path with prefix")
	}
}
