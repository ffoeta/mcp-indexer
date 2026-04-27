package store

// Kind — высокоуровневый тип ноды.
const (
	KindObject = "object"
	KindMethod = "method"
)

// Subkinds — конкретный подвид (зависит от языка).
const (
	SubClass     = "class"
	SubInterface = "interface"
	SubEnum      = "enum"
	SubStruct    = "struct"
	SubTrait     = "trait"

	SubFn     = "fn"     // свободная функция (top-level)
	SubMethod = "method" // метод объекта
	SubCtor   = "ctor"
	SubModule = "module" // synthetic module-method (Python/JS-модули)
	SubInit   = "init"   // synthetic class-init (Java field/static initializers)
)

// Scope — где живёт нода.
const (
	ScopeGlobal = "global"
	ScopeLocal  = "local"
	ScopeMember = "member"
)

// Confidence — уровень уверенности резолюции рёбер.
const (
	ConfSameFile = 100
	ConfImport   = 70
	ConfGlobal   = 40
	ConfNone     = 0
)

// Relation — тип наследования.
const (
	RelExtends    = "extends"
	RelImplements = "implements"
)

// DocKind — тип документа в FTS5.
const (
	DocNode = "node"
	DocFile = "file"
)

// FileRow — строка таблицы files.
type FileRow struct {
	FileID  string
	ShortID int64
	Key     string
	RelPath string
	Lang    string
	Package string
	Hash    string
}

// NodeRow — строка таблицы nodes.
type NodeRow struct {
	NodeID    string
	ShortID   int64
	FileID    string
	Kind      string // KindObject | KindMethod
	Subkind   string
	Name      string
	FQN       string
	OwnerID   string // "" если без owner
	Scope     string
	Signature string
	Doc       string
	StartLine int
	EndLine   int
}

// CallEdge — строка edges_calls. CalleeID="" → unresolved.
type CallEdge struct {
	CallerID    string
	CalleeID    string
	CalleeName  string
	CalleeOwner string
	Line        int
	Confidence  int
}

// InheritEdge — строка edges_inherits. ParentID="" → unresolved.
type InheritEdge struct {
	ChildID    string
	ParentID   string
	ParentHint string
	Relation   string // RelExtends | RelImplements
}

// ImportEdge — строка edges_imports. TargetFileID="" → external.
type ImportEdge struct {
	FileID       string
	TargetFileID string
	Raw          string
}

// SearchDoc — запись для FTS5 search_idx.
type SearchDoc struct {
	DocID   string
	DocKind string // DocNode | DocFile
	Name    string // pre-tokenized
	FQN     string // pre-tokenized
	Path    string // pre-tokenized
}