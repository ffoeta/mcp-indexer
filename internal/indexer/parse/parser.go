package parse

import "fmt"

// Parser извлекает структурированную информацию из файла.
type Parser interface {
	Parse(absPath string) (*ParseResult, error)
}

// SyntheticModuleName — имя synthetic method, к которому атрибутируются
// module-level вызовы Python и top-level скрипт-кода других динамических языков.
const SyntheticModuleName = "<module>"

// SyntheticInitName — имя synthetic method для атрибуции вызовов из
// field-initializer-ов, static-инициализаторов и instance-блоков Java/JVM.
// FQN такого узла = <classFQN>.<init>.
const SyntheticInitName = "<init>"

// ParseResult — результат парсинга файла.
type ParseResult struct {
	Package  string       // объявленный пакет/модуль (java: "com.x.svc"; py: "" — вычисляется в engine)
	Imports  []ImportRef
	Objects  []ObjectDef
	Methods  []MethodDef  // включая synthetic <module> для динамических языков
	Calls    []CallRef
	VarTypes []VarType    // для Pass 2 резолюции
}

// ImportRef — одна строчка import.
type ImportRef struct {
	Raw   string // "os.path", "com.x.OrderRepo", "./utils"
	Alias string // alias для importMap; для default import == последняя часть Raw
}

// ObjectDef — class / interface / enum / struct / trait.
type ObjectDef struct {
	Name      string
	FQN       string    // canonical, package-aware
	Subkind   string    // class|interface|enum|struct|trait
	Bases     []BaseRef
	Doc       string    // первая строка docstring/Javadoc/JSDoc, обрезка ≤120 chars
	StartLine int
	EndLine   int
}

// BaseRef — ссылка на базовый объект (extends/implements).
type BaseRef struct {
	Name     string // как написано в коде
	Relation string // RelExtends | RelImplements (см. store.RelExtends/Implements)
}

// MethodDef — функция или метод.
type MethodDef struct {
	Name      string
	FQN       string
	OwnerFQN  string // "" для свободной функции / synthetic <module>
	Subkind   string // fn|method|ctor|module
	Scope     string // global|local|member  (см. store.ScopeGlobal/Local/Member)
	Signature string
	Doc       string
	StartLine int
	EndLine   int
}

// CallRef — место вызова.
//
// CallerFQN — обязательное поле: указывает, какому method-у принадлежит вызов.
// Для top-level/module-level кода — synthetic <module>.
//
// CalleeOwner — receiver / module / класс, как видит парсер:
//
//	"" для bare-call (foo()),
//	"os.path" для атрибут-цепочки,
//	"OrderRepo" для имени-объекта (резолвится через VarTypes/Imports в Pass 2).
type CallRef struct {
	CallerFQN   string
	CalleeName  string
	CalleeOwner string
	Line        int
}

// VarType — статически выводимое соответствие переменная→тип в локальной scope.
// Используется в Pass 2 для резолюции вызовов вида "var.method()".
type VarType struct {
	ScopeFQN string // FQN метода/функции, в которой видно переменную; "" для file-scope
	VarName  string
	TypeName string // simple type name из source; в Pass 2 матчится через importMap
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
