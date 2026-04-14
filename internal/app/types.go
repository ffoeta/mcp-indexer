package app

// SearchLimits — лимиты по типам результатов для search().
type SearchLimits struct {
	Sym  int `json:"sym"`
	File int `json:"file"`
	Mod  int `json:"mod"`
}

func DefaultSearchLimits() SearchLimits {
	return SearchLimits{Sym: 20, File: 10, Mod: 5}
}

// SearchResponse — ответ search().
type SearchResponse struct {
	Sym  [][]interface{} `json:"sym"`  // [symbolId, kind, name, fileKey, startLine, endLine]
	File [][]interface{} `json:"file"` // [fileKey]
	Mod  [][]interface{} `json:"mod"`  // [moduleId, moduleName]
}
