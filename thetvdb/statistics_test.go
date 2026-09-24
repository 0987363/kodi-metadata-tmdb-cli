package thetvdb

import (
	"context"
	"net/http"
	"testing"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

func TestStatisticsCountsBatchAndSupplementOnlyOnHTTP(t *testing.T) {
	server, counts := batchServer(t, batchPages())
	p := New(server.Client(), server.URL, "zh-CN", 0)
	req := metadata.SeriesRequest{Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, Slug: "26882341-show"}, Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}, {Season: 1, Episode: 2}}}
	if _, err := p.FetchSeries(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	req.Details = true
	if _, err := p.FetchSeries(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := p.FetchSeries(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	got := p.Statistics()
	if len(counts) != 5 || got.HTTPAttempts != 5 || got.BatchRequests != 2 || got.DetailRequests != 2 || got.SearchRequests != 0 {
		t.Fatalf("页面请求分类或缓存复用错误：路径=%v 统计=%+v", counts, got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := p.request(ctx, http.MethodGet, server.URL+"/search?query=x", nil); err == nil {
		t.Fatal("取消后的请求应返回错误")
	}
	if p.Statistics() != got {
		t.Fatalf("取消的请求不应计数：%+v", p.Statistics())
	}
}
