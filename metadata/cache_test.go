package metadata

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadCacheRejectsPriorEnvelopeVersions(t *testing.T) {
	for _, version := range []int{0, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "cache.json")
			stored := map[string]any{"saved_at": time.Now(), "value": []Candidate{{Ref: Ref{Provider: "tmdb", Kind: Movie, ID: "11"}}}}
			if version != 0 {
				stored["version"] = version
			}
			data, err := json.Marshal(stored)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, data, 0644); err != nil {
				t.Fatal(err)
			}
			_, hit, err := readCache[[]Candidate](file, time.Hour)
			if err != nil || hit {
				t.Fatalf("旧首屏候选缓存仍被使用：hit=%v err=%v", hit, err)
			}
		})
	}
}

func TestWriteCacheUsesCurrentEnvelope(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cache.json")
	if err := writeCache(file, time.Hour, []Candidate{{Ref: Ref{Provider: "tmdb", Kind: Movie, ID: "11"}}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Version != 3 {
		t.Fatalf("新缓存缺少版本隔离：%s", data)
	}
	value, hit, err := readCache[[]Candidate](file, time.Hour)
	if err != nil || !hit || len(value) != 1 || value[0].Ref.ID != "11" {
		t.Fatalf("新候选缓存不能读取：%+v hit=%v err=%v", value, hit, err)
	}
}

func TestCacheKeyDoesNotReusePriorCandidatePages(t *testing.T) {
	provider := &testProvider{name: "tmdb", scope: "zh"}
	req := Request{Kind: Movie, Query: Query{Kind: Movie, Title: "作品"}}
	legacyKey, err := json.Marshal(struct {
		Version          int
		Scope, Operation string
		Request          Request
	}{2, "zh", "search", req})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	oldFile := filepath.Join(root, "cache", "tmdb", fmt.Sprintf("%x.json", sha256.Sum256(legacyKey)))
	if cacheFile(root, provider, "search", req) == oldFile {
		t.Fatal("新完整候选列表仍使用旧首屏缓存键")
	}
}
