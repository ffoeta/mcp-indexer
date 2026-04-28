package app

// ───────── search ─────────

type SearchHitOut struct {
	ID   string `json:"id"`             // short: "m412"/"o7"/"f3"
	K    string `json:"k"`              // method|object|file
	Name string `json:"name,omitempty"`
	FQN  string `json:"fqn,omitempty"`
	File string `json:"file,omitempty"` // rel_path
	L    int    `json:"l,omitempty"`    // start_line (для node)
}

type SearchOut struct {
	Hits []SearchHitOut `json:"hits"`
}

// ───────── peek ─────────

// PeekObjectOut — сводка object (class/interface/enum).
type PeekObjectOut struct {
	ID          string   `json:"id"`
	K           string   `json:"k"` // "object"
	Subk        string   `json:"subk"`
	Name        string   `json:"name"`
	FQN         string   `json:"fqn,omitempty"`
	File        string   `json:"file"`
	L           int      `json:"l"`
	End         int      `json:"end"`
	Doc         string   `json:"doc,omitempty"`
	Owner       string   `json:"owner,omitempty"` // short ID nested-parent (если есть)
	Methods     []string `json:"methods,omitempty"`
	Extends     []string `json:"extends,omitempty"`
	Implements  []string `json:"implements,omitempty"`
	ExtendedBy  int      `json:"extended_by,omitempty"`
}

// PeekMethodOut — сводка method/function.
type PeekMethodOut struct {
	ID              string `json:"id"`
	K               string `json:"k"` // "method"
	Subk            string `json:"subk"`
	Name            string `json:"name"`
	FQN             string `json:"fqn,omitempty"`
	Owner           string `json:"owner,omitempty"` // short ID объекта-владельца
	File            string `json:"file"`
	L               int    `json:"l"`
	End             int    `json:"end"`
	Sig             string `json:"sig,omitempty"`
	Doc             string `json:"doc,omitempty"`
	Calls           int    `json:"calls,omitempty"`
	CalledBy        int    `json:"called_by,omitempty"`
	UnresolvedCalls int    `json:"unresolved_calls,omitempty"`
}

// PeekFileOut — сводка файла без objects/methods deep dive.
type PeekFileOut struct {
	ID      string `json:"id"`
	K       string `json:"k"` // "file"
	Path    string `json:"path"`
	Lang    string `json:"lang,omitempty"`
	Pkg     string `json:"pkg,omitempty"`
	Objects int    `json:"objects"`
	Methods int    `json:"methods"`
	Imports int    `json:"imports"`
}

// ───────── walk ─────────

type WalkItem struct {
	From string `json:"from,omitempty"` // short-id (для in-direction)
	To   string `json:"to,omitempty"`   // short-id (для out-direction)
	Name string `json:"name,omitempty"`
	FQN  string `json:"fqn,omitempty"`
	File string `json:"file,omitempty"`
	L    int    `json:"l,omitempty"`
	Hint string `json:"hint,omitempty"` // для unresolved (callee/parent_hint/import.raw)
	Line int    `json:"line,omitempty"` // строка использования (calls)
	Conf int    `json:"conf,omitempty"` // только calls
	Rel  string `json:"rel,omitempty"`  // только inherits
}

type WalkOut struct {
	Items []WalkItem `json:"items"`
	Total int        `json:"total"`
}

// ───────── code ─────────

type CodeOut struct {
	ID    string `json:"id"`
	File  string `json:"file"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Src   string `json:"src"`
}

// ───────── file overview ─────────

type FileImportOut struct {
	Raw string `json:"raw"`
	Tgt string `json:"tgt,omitempty"` // short-id целевого файла; пусто = external
}

type NodeBriefOut struct {
	ID      string `json:"id"`
	Subk    string `json:"subk,omitempty"`
	Name    string `json:"name"`
	L       int    `json:"l"`
	End     int    `json:"end,omitempty"`
	Methods int    `json:"methods,omitempty"` // method count для object
	Owner   string `json:"owner,omitempty"`   // short-id владельца
}

type FileOut struct {
	ID      string          `json:"id"`
	Path    string          `json:"path"`
	Lang    string          `json:"lang,omitempty"`
	Pkg     string          `json:"pkg,omitempty"`
	Imports []FileImportOut `json:"imports,omitempty"`
	Objects []NodeBriefOut  `json:"objects,omitempty"`
	Methods []NodeBriefOut  `json:"methods,omitempty"`
}

// ───────── tree ─────────

type TreeFileOut struct {
	ID      string `json:"id"`   // short fileId, e.g. "f3"
	Path    string `json:"path"` // rel_path
	Lang    string `json:"lang,omitempty"`
	Pkg     string `json:"pkg,omitempty"`
	Objects int    `json:"objects,omitempty"`
	Methods int    `json:"methods,omitempty"`
}

type TreeOut struct {
	Files []TreeFileOut `json:"files"`
}

// ───────── stats ─────────

type StatsOut struct {
	Files           int `json:"files"`
	Objects         int `json:"objects"`
	Methods         int `json:"methods"`
	CallsResolved   int `json:"calls_resolved"`
	CallsUnresolved int `json:"calls_unresolved"`
	Inherits        int `json:"inherits"`
	ImportsResolved int `json:"imports_resolved"`
	ImportsExternal int `json:"imports_external"`
	SearchDocs      int `json:"search_docs"`
}

// ───────── graph ─────────

type GraphNodeOut struct {
	ID    string `json:"id"`             // short: "f12" / "o7" / "m412"
	K     string `json:"k"`              // file | object | method
	Name  string `json:"name"`           // basename (file) или name (node)
	Path  string `json:"path,omitempty"` // rel_path; для node — путь файла-владельца
	Lang  string `json:"lang,omitempty"`
	Subk  string `json:"subk,omitempty"`
	L     int    `json:"l,omitempty"`     // start_line для node
	Owner string `json:"owner,omitempty"` // short-id object-владельца (если есть)
	File  string `json:"file,omitempty"`  // short-id файла, в котором лежит node
}

type GraphEdgeOut struct {
	From string `json:"from"`
	To   string `json:"to"`
	T    string `json:"t"`             // calls | inherits | imports | defines
	Rel  string `json:"rel,omitempty"` // extends | implements (для inherits)
}

type GraphOut struct {
	Nodes []GraphNodeOut `json:"nodes"`
	Edges []GraphEdgeOut `json:"edges"`
}

// ───────── services ─────────

type ServiceOut struct {
	ID           string   `json:"id"`
	Root         string   `json:"root"`
	Description  string   `json:"description,omitempty"`
	MainEntities []string `json:"mainEntities,omitempty"`
}