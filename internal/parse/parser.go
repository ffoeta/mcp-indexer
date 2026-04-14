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

// CallRef — уникальный вызов функции/конструктора с эвристической резолюцией.
// Module и Local взаимоисключающие; если оба пусты — вызов не резолвится.
type CallRef struct {
	Name   string // имя как написано в исходнике (e.g. "os.path.join")
	Line   int    // первое место вызова
	Module string // резолвится в модуль (e.g. "os.path")
	Local  string // резолвится в локальный символ того же файла
}

// SymbolDef описывает символ верхнего уровня или метод.
type SymbolDef struct {
	Kind      string      // "class", "function", "method"
	Name      string
	Qualified string      // e.g. "ClassName.method_name"
	StartLine int
	EndLine   int
	Bases     []string    // только для class
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
