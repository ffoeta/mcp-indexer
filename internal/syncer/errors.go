package syncer

import "fmt"

// Pos — позиция в файле (для parse-ошибок).
type Pos struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

// SyncError описывает ошибку при синхронизации конкретного файла.
type SyncError struct {
	Key     string `json:"key"`
	AbsPath string `json:"absPath,omitempty"`
	Stage   string `json:"stage"`  // scan|hash|parse|index
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Pos     *Pos   `json:"pos,omitempty"`
}

func (e *SyncError) Error() string {
	if e.Pos != nil {
		return fmt.Sprintf("[%s] %s:%d:%d %s: %s", e.Stage, e.Key, e.Pos.Line, e.Pos.Col, e.Code, e.Message)
	}
	return fmt.Sprintf("[%s] %s %s: %s", e.Stage, e.Key, e.Code, e.Message)
}
