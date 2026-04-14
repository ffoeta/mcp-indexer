package services

// ServiceEntry — запись в registry.json.
type ServiceEntry struct {
	RootAbs string `json:"rootAbs"`
	Name    string `json:"name,omitempty"`
}
