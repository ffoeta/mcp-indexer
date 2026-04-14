package syncer

import (
	"fmt"
	"mcp-indexer/internal/services"
)

// PrepareSyncResult — результат prepareSync (только чтение, ничего не пишет).
type PrepareSyncResult struct {
	Added         int         `json:"added"`
	MaybeModified int         `json:"maybeModified"`
	Deleted       int         `json:"deleted"`
	Errors        []SyncError `json:"errors,omitempty"`
}

// PrepareSync выполняет stat-only скан и diff, ничего не меняет.
func PrepareSync(svc services.ServiceEntry, svcID string) (*PrepareSyncResult, error) {
	cfg, err := services.LoadConfig(services.ConfigPath(svcID))
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	matcher, err := services.LoadMatcher(services.IgnoreFilePath(svcID))
	if err != nil {
		return nil, fmt.Errorf("load ignore: %w", err)
	}

	current, err := Scan(svc.RootAbs, cfg, matcher)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	saved, err := services.LoadFileStat(services.FileStatPath(svcID))
	if err != nil {
		return nil, fmt.Errorf("load file-stat: %w", err)
	}

	diff := DiffStat(current, saved)
	return &PrepareSyncResult{
		Added:         len(diff.Added),
		MaybeModified: len(diff.MaybeModified),
		Deleted:       len(diff.Deleted),
	}, nil
}
