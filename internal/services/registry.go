package services

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Registry держит map serviceId -> ServiceEntry.
type Registry struct {
	path string
	mu   sync.RWMutex
	m    map[string]ServiceEntry
}

func LoadRegistry(path string) (*Registry, error) {
	r := &Registry{path: path, m: make(map[string]ServiceEntry)}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}
	if err := json.Unmarshal(data, &r.m); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	return r, nil
}

func (r *Registry) Save() error {
	r.mu.RLock()
	data, err := json.MarshalIndent(r.m, "", "  ")
	r.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	if err := os.MkdirAll(AppHome(), 0o755); err != nil {
		return err
	}
	return atomicWriteFile(r.path, data)
}

func (r *Registry) Add(id string, entry ServiceEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.m[id]; exists {
		return fmt.Errorf("service %q already registered", id)
	}
	r.m[id] = entry
	return nil
}

func (r *Registry) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.m[id]; !exists {
		return fmt.Errorf("service %q not found", id)
	}
	delete(r.m, id)
	return nil
}

func (r *Registry) Get(id string) (ServiceEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.m[id]
	return e, ok
}

func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.m))
	for id := range r.m {
		ids = append(ids, id)
	}
	return ids
}

// ListFull возвращает копию всех записей (id → entry).
func (r *Registry) ListFull() map[string]ServiceEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]ServiceEntry, len(r.m))
	for id, e := range r.m {
		out[id] = e
	}
	return out
}

// UpdateMeta обновляет description и mainEntities у существующей записи.
// Пустые значения игнорируются (не затирают существующие).
func (r *Registry) UpdateMeta(id, description string, mainEntities []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.m[id]
	if !ok {
		return fmt.Errorf("service %q not found", id)
	}
	if description != "" {
		e.Description = description
	}
	if len(mainEntities) > 0 {
		e.MainEntities = mainEntities
	}
	r.m[id] = e
	return nil
}
