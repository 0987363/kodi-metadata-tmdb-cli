package metadata

import (
	"context"
	"testing"
	"time"
)

type seriesCacheProvider struct {
	testProvider
	seriesCalls int
	incomplete  bool
}

func (p *seriesCacheProvider) FetchSeries(_ context.Context, req SeriesRequest) (*SeriesResult, error) {
	p.seriesCalls++
	_, result := validSeriesFixture()
	if req.Details {
		result.Work.Title = "完整详情"
	}
	if p.incomplete {
		result.Episodes = result.Episodes[:1]
	}
	return result, nil
}

func TestSeriesCacheCanonicalizesDuplicateKeysAndSeparatesDetailsAndScope(t *testing.T) {
	p := &seriesCacheProvider{testProvider: testProvider{name: "tmdb", scope: "zh"}}
	m := NewManager([]Provider{p}, time.Hour, nil)
	req, _ := validSeriesFixture()
	root := t.TempDir()
	if _, err := m.fetchSeries(context.Background(), p, req, root); err != nil {
		t.Fatal(err)
	}
	req.Episodes = []EpisodeKey{req.Episodes[1], req.Episodes[0], req.Episodes[0]}
	if _, err := m.fetchSeries(context.Background(), p, req, root); err != nil {
		t.Fatal(err)
	}
	if p.seriesCalls != 1 {
		t.Fatalf("重排/重复单集未复用事实: %d", p.seriesCalls)
	}
	req.Details = true
	got, err := m.fetchSeries(context.Background(), p, req, root)
	if err != nil || got.Work.Title != "完整详情" || p.seriesCalls != 2 {
		t.Fatalf("基础缓存冒充完整详情: %+v %v", got, err)
	}
	p.scope = "en"
	if _, err := m.fetchSeries(context.Background(), p, req, root); err != nil {
		t.Fatal(err)
	}
	if p.seriesCalls != 3 {
		t.Fatal("跨语言缓存串用")
	}
}

func TestSeriesCacheDoesNotPersistIncompleteResultsOrCallAfterCancellation(t *testing.T) {
	p := &seriesCacheProvider{testProvider: testProvider{name: "tmdb", scope: "zh"}, incomplete: true}
	m := NewManager([]Provider{p}, time.Hour, nil)
	req, _ := validSeriesFixture()
	root := t.TempDir()
	if _, err := m.fetchSeries(context.Background(), p, req, root); err == nil {
		t.Fatal("缺集结果被缓存")
	}
	p.incomplete = false
	if _, err := m.fetchSeries(context.Background(), p, req, root); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.fetchSeries(ctx, p, req, root); err == nil {
		t.Fatal("取消后仍返回成功")
	}
	if p.seriesCalls != 2 {
		t.Fatalf("请求次数异常: %d", p.seriesCalls)
	}
}
