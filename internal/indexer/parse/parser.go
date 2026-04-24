package parse

import "fmt"

// Parser извлекает структурированную информацию из файла.
type Parser interface {
	Parse(absPath string) (*ParseResult, error)
}

// ParseResult — результат парсинга файла.
type ParseResult struct {
	Imports []string
	Symbols []SymbolDef
	Calls   []CallRef
}

// CallRef — вызов функции/конструктора внутри файла.
type CallRef struct {
	Caller string // qualified name вызывающего символа (пусто = уровень файла)
	Line   int    // строка вызова
	Module string // резолвится в модуль (e.g. "os.path") — для symbols_used.json
	Local  string // резолвится в локальный символ того же файла — для calls edges
}

// SymbolDef описывает символ верхнего уровня или метод.
type SymbolDef struct {
	Kind      string   // "class", "function", "method"
	Name      string
	Qualified string   // e.g. "ClassName.method_name"
	Parent    string   // qualified name родительского класса (для методов)
	StartLine int
	EndLine   int
	Bases     []string // только для class
}

// ParseError — ошибка с позицией (SyntaxError и т.д.).
type ParseError struct {
	Message string
	Line    int
	Col     int
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s (line %d, col %d)", e.Message, e.Line, e.Col)
	}
	return e.Message
}
