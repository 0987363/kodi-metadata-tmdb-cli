package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"fengqi/kodi-metadata-tmdb-cli/config"
	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

func TestStatisticsCountsActualBatchRequestsAndCacheReuse(t *testing.T) {
	var received atomic.Int64
	p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		payload := map[string]any{"id": 9, "name": "节目", "number_of_seasons": 1, "number_of_episodes": 1}
		for _, path := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
			switch path {
			case "season/1":
				payload[path] = map[string]any{"season_number": 1, "episodes": []any{map[string]any{"id": 101, "show_id": 9, "season_number": 1, "episode_number": 1, "name": "第一集"}}}
			default:
				payload[path] = validBatchObject(path)
			}
		}
		json.NewEncoder(w).Encode(payload)
	})
	req := metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}}}
	for range 2 {
		if _, err := p.FetchSeries(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	got := p.(interface{ Statistics() metadata.SourceStats }).Statistics()
	if received.Load() != 1 || got.HTTPAttempts != 1 || got.BatchRequests != 1 || got.Retries != 0 || got.DetailRequests != 0 {
		t.Fatalf("缓存复用或批量计数错误：请求=%d 统计=%+v", received.Load(), got)
	}
}

func TestStatisticsCountsRetryAttempts(t *testing.T) {
	var received atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if received.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	p := NewMetadataProvider(&config.TmdbConfig{ApiHost: server.URL, ApiKey: "secret", Language: "zh-CN", RetryCount: 1, TimeoutSeconds: 3}).(*metadataProvider)
	var result map[string]any
	if err := p.requestJSON(context.Background(), "/3/search/movie", url.Values{}, &result); err != nil {
		t.Fatal(err)
	}
	got := p.Statistics()
	if received.Load() != 2 || got.HTTPAttempts != 2 || got.Retries != 1 || got.SearchRequests != 2 {
		t.Fatalf("重试请求计数错误：请求=%d 统计=%+v", received.Load(), got)
	}
}
