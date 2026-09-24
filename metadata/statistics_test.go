package metadata

import (
	"context"
	"testing"
	"time"
)

func TestManagerStatisticsCountsJudgmentsAcrossCacheReuse(t *testing.T) {
	p := &testProvider{name: "tmdb", scope: "zh"}
	m := NewManager([]Provider{p}, time.Hour, &testJudge{})
	req := Request{Kind: Movie, Ref: Ref{Provider: "tmdb", Kind: Movie, ID: "42"}}
	root := t.TempDir()
	for range 2 {
		if _, err := m.Resolve(context.Background(), req, root); err != nil {
			t.Fatal(err)
		}
	}
	got := m.Statistics()["tmdb"]
	if got.Decisions != 2 || got.HTTPAttempts != 0 || got.SearchRequests != 0 {
		t.Fatalf("判断次数或来源请求计数错误：%+v", got)
	}
}
