package store

// FileRow соответствует таблице files.
type FileRow struct {
	FileID  string
	Key     string
	RelPath string
	Lang    string
	Hash    string
}

// ImportRow соответствует таблице imports.
type ImportRow struct {
	FileID string
	Imp    string
}

// SymbolRow соответствует таблице symbols.
type SymbolRow struct {
	SymbolID  string
	FileID    string
	Kind      string
	Name      string
	Qualified string
	StartLine int
	EndLine   int
}

// EdgeRow соответствует таблице edges.
type EdgeRow struct {
	Type       string
	FromID     string
	ToID       string
	Confidence int
	Aux        string
}

// TermPosting соответствует таблице term_postings.
type TermPosting struct {
	Term  string
	DocID string
	Weight float64
}
