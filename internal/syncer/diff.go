package syncer

import (
	"mcp-indexer/internal/services"
	"sort"
)

// StatDiffResult — результат stat-diff (prepareSync).
type StatDiffResult struct {
	Added         []string // keys
	MaybeModified []string // keys (mtime или size изменились)
	Deleted       []string // keys
}

// DiffStat сравнивает текущий скан с file-stat.json.
func DiffStat(current []FileEntry, saved services.FileStat) StatDiffResult {
	var res StatDiffResult
	curKeys := make(map[string]struct{}, len(current))

	for _, f := range current {
		curKeys[f.Key] = struct{}{}
		if st, ok := saved[f.Key]; !ok {
			res.Added = append(res.Added, f.Key)
		} else {
			if f.ModTime.UnixNano() != st[0] || f.Size != st[1] {
				res.MaybeModified = append(res.MaybeModified, f.Key)
			}
		}
	}
	for key := range saved {
		if _, ok := curKeys[key]; !ok {
			res.Deleted = append(res.Deleted, key)
		}
	}

	sort.Strings(res.Added)
	sort.Strings(res.MaybeModified)
	sort.Strings(res.Deleted)
	return res
}

// HashDiffResult — результат hash-diff (doSync).
type HashDiffResult struct {
	Added    []FileEntry // new key
	Modified []FileEntry // hash changed
	Deleted  []string    // keys не в текущем скане
	// ReadErrors: ключи, которые были в savedHash, но не удалось прочитать/хэшировать
	ReadErrors []SyncError
}

// DiffHash сравнивает текущий скан с file-map.json через blake3.
// Файлы с ошибкой чтения считаются deleted + error.
func DiffHash(current []FileEntry, saved services.FileMap) HashDiffResult {
	var res HashDiffResult
	curKeys := make(map[string]struct{}, len(current))

	for _, f := range current {
		curKeys[f.Key] = struct{}{}
		hash, err := HashFile(f.AbsPath)
		if err != nil {
			// ошибка чтения → deleted + error
			if _, wasSaved := saved[f.Key]; wasSaved {
				res.Deleted = append(res.Deleted, f.Key)
			}
			res.ReadErrors = append(res.ReadErrors, SyncError{
				Key:     f.Key,
				AbsPath: f.AbsPath,
				Stage:   "hash",
				Code:    "HASH_ERROR",
				Message: err.Error(),
			})
			continue
		}

		if savedHash, ok := saved[f.Key]; !ok {
			res.Added = append(res.Added, f)
		} else if savedHash != hash {
			res.Modified = append(res.Modified, f)
		}
	}

	for key := range saved {
		if _, ok := curKeys[key]; !ok {
			res.Deleted = append(res.Deleted, key)
		}
	}

	sort.Slice(res.Added, func(i, j int) bool { return res.Added[i].Key < res.Added[j].Key })
	sort.Slice(res.Modified, func(i, j int) bool { return res.Modified[i].Key < res.Modified[j].Key })
	sort.Strings(res.Deleted)
	return res
}

// DiffHashCandidates — как DiffHash, но хэширует только файлы из candidates.
// Файлы вне candidates считаются неизменёнными (не попадают в Added/Modified).
// Используется совместно с DiffStat для избежания лишних IO.
func DiffHashCandidates(current []FileEntry, saved services.FileMap, candidates map[string]struct{}) HashDiffResult {
	var res HashDiffResult
	curKeys := make(map[string]struct{}, len(current))

	for _, f := range current {
		curKeys[f.Key] = struct{}{}

		if _, isCandidate := candidates[f.Key]; !isCandidate {
			// mtime/size не изменились — пропускаем хэш
			continue
		}

		hash, err := HashFile(f.AbsPath)
		if err != nil {
			if _, wasSaved := saved[f.Key]; wasSaved {
				res.Deleted = append(res.Deleted, f.Key)
			}
			res.ReadErrors = append(res.ReadErrors, SyncError{
				Key:     f.Key,
				AbsPath: f.AbsPath,
				Stage:   "hash",
				Code:    "HASH_ERROR",
				Message: err.Error(),
			})
			continue
		}

		if savedHash, ok := saved[f.Key]; !ok {
			res.Added = append(res.Added, f)
		} else if savedHash != hash {
			res.Modified = append(res.Modified, f)
		}
		// savedHash == hash: mtime изменился, но содержимое то же — пропускаем
	}

	for key := range saved {
		if _, ok := curKeys[key]; !ok {
			res.Deleted = append(res.Deleted, key)
		}
	}

	sort.Slice(res.Added, func(i, j int) bool { return res.Added[i].Key < res.Added[j].Key })
	sort.Slice(res.Modified, func(i, j int) bool { return res.Modified[i].Key < res.Modified[j].Key })
	sort.Strings(res.Deleted)
	return res
}
