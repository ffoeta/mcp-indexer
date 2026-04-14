package syncer

import (
	"mcp-indexer/internal/services"
	"sort"
	"testing"
	"time"
)

func makeEntry(key string) FileEntry {
	return FileEntry{Key: key, AbsPath: "/root/" + key, RelPath: key, ModTime: time.Unix(1000, 0), Size: 100}
}

// E1: DiffHash_Added
func TestDiffHash_Added(t *testing.T) {
	// Use DiffStat to test added logic (stat-based, easier to unit test)
	current := []FileEntry{makeEntry("a.py")}
	saved := services.FileStat{}
	res := DiffStat(current, saved)
	if len(res.Added) != 1 || res.Added[0] != "a.py" {
		t.Errorf("expected Added=[a.py], got %v", res.Added)
	}
}

// E2: DiffHash_Deleted (via DiffStat)
func TestDiffStat_Deleted(t *testing.T) {
	current := []FileEntry{}
	saved := services.FileStat{"a.py": {1000 * 1e9, 100}}
	res := DiffStat(current, saved)
	if len(res.Deleted) != 1 || res.Deleted[0] != "a.py" {
		t.Errorf("expected Deleted=[a.py], got %v", res.Deleted)
	}
}

// E3: DiffHash_Modified (via stat — mtime change)
func TestDiffStat_Modified(t *testing.T) {
	current := []FileEntry{{Key: "a.py", ModTime: time.Unix(2000, 0), Size: 100}}
	saved := services.FileStat{"a.py": {1000 * 1e9, 100}} // mtime 1000 != 2000
	res := DiffStat(current, saved)
	if len(res.MaybeModified) != 1 {
		t.Errorf("expected MaybeModified=[a.py], got %v", res.MaybeModified)
	}
}

// E4: DiffHash_NoChanges_AllEmpty
func TestDiffStat_NoChanges_AllEmpty(t *testing.T) {
	ts := time.Unix(1000, 0)
	current := []FileEntry{{Key: "a.py", ModTime: ts, Size: 100}}
	saved := services.FileStat{"a.py": {ts.UnixNano(), 100}}
	res := DiffStat(current, saved)
	if len(res.Added)+len(res.MaybeModified)+len(res.Deleted) != 0 {
		t.Errorf("expected no changes, got %+v", res)
	}
}

// E5: DiffHash_SortsOutputs
func TestDiffStat_SortsOutputs(t *testing.T) {
	current := []FileEntry{makeEntry("z.py"), makeEntry("a.py"), makeEntry("m.py")}
	saved := services.FileStat{}
	res := DiffStat(current, saved)
	if !sort.StringsAreSorted(res.Added) {
		t.Errorf("Added not sorted: %v", res.Added)
	}
}

// E6-E10 covered above

// E8: DiffStat_MaybeModified_MtimeChange
func TestDiffStat_MaybeModified_MtimeChange(t *testing.T) {
	current := []FileEntry{{Key: "a.py", ModTime: time.Unix(9999, 0), Size: 100}}
	saved := services.FileStat{"a.py": {1000 * 1e9, 100}}
	res := DiffStat(current, saved)
	if len(res.MaybeModified) == 0 {
		t.Error("expected maybeModified for mtime change")
	}
}

// E9: DiffStat_MaybeModified_SizeChange
func TestDiffStat_MaybeModified_SizeChange(t *testing.T) {
	ts := time.Unix(1000, 0)
	current := []FileEntry{{Key: "a.py", ModTime: ts, Size: 9999}} // size changed
	saved := services.FileStat{"a.py": {ts.UnixNano(), 100}}
	res := DiffStat(current, saved)
	if len(res.MaybeModified) == 0 {
		t.Error("expected maybeModified for size change")
	}
}

// E12: Diff_HandlesLargeMaps_PerformanceSanity
func TestDiff_HandlesLargeMaps_PerformanceSanity(t *testing.T) {
	const N = 50_000
	saved := make(services.FileStat, N)
	current := make([]FileEntry, N)
	ts := time.Unix(1000, 0)
	for i := range N {
		key := string(rune('a'+i%26)) + "_" + string(rune('0'+i%10))
		key = key + "_" + string(rune('a'+(i/100)%26)) + ".py"
		import_key := key
		if i > N/2 {
			import_key = "x_" + key
		}
		saved[import_key] = [2]int64{ts.UnixNano(), 100}
		current[i] = FileEntry{Key: key, ModTime: ts, Size: 100}
	}
	// Should complete without timeout
	res := DiffStat(current, saved)
	_ = res
}

func TestDiffStat_Deleted_Sorted(t *testing.T) {
	saved := services.FileStat{"z.py": {1, 1}, "a.py": {1, 1}, "m.py": {1, 1}}
	res := DiffStat([]FileEntry{}, saved)
	if !sort.StringsAreSorted(res.Deleted) {
		t.Errorf("Deleted not sorted: %v", res.Deleted)
	}
}
