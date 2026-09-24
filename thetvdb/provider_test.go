package thetvdb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func servePages(t *testing.T, pages map[string]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dereferrer/series/371065" {
			http.Redirect(w, r, "/series/26882341-show", http.StatusFound)
			return
		}
		if page, ok := pages[r.URL.Path]; ok {
			fmt.Fprint(w, page)
			return
		}
		t.Errorf("意外请求：%s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFetchShowByNumericIDWithoutSearch(t *testing.T) {
	server := servePages(t, map[string]string{"/series/26882341-show": fixture(t, "series.html")})
	provider := New(server.Client(), server.URL, "zh-CN", 0)
	record, err := provider.Fetch(context.Background(), metadata.Request{Kind: metadata.Show, Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: "371065"}})
	if err != nil {
		t.Fatal(err)
	}
	if record.Ref.ID != "371065" || record.Ref.Slug != "26882341-show" || record.Ref.Provider != "thetvdb" || record.Title != "坑王驾到" {
		t.Fatalf("节目引用或标题错误：%+v", record)
	}
	if record.Plot != "用于解析测试的节目简介。" || record.Premiered != "2016-11-27" || record.RuntimeMinutes != 55 || record.LastAired != "2020-09-05" {
		t.Fatalf("详情解析错误：%+v", record)
	}
	ids := make(map[string]string)
	for _, id := range record.ExternalIDs {
		ids[id.Type] = id.Value
	}
	if ids["tvdb"] != "371065" || ids["tmdb"] != "74747" || ids["imdb"] != "tt1234567" {
		t.Fatalf("跨站编号错误：%v", ids)
	}
	if record.SeasonCount != 1 || record.EpisodeCount != 184 || len(record.Seasons) != 2 || len(record.Artwork) != 2 {
		t.Fatalf("季或图片解析错误：%+v", record)
	}
	if record.Artwork[0].URL == "thumbnail.jpg" {
		t.Fatal("错误使用了缩略图")
	}
}

func TestFetchFailuresRemainDistinct(t *testing.T) {
	for _, status := range []int{403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			_, err := New(server.Client(), server.URL, "en", 0).Fetch(context.Background(), metadata.Request{Kind: metadata.Show, Ref: metadata.Ref{ID: "371065"}})
			if err == nil || errors.Is(err, metadata.ErrNotFound) != (status == 404) {
				t.Fatalf("状态%d错误分类：%v", status, err)
			}
		})
	}
	server := servePages(t, map[string]string{"/series/26882341-show": "<html><h1>Login</h1></html>"})
	_, err := New(server.Client(), server.URL, "en", 0).Fetch(context.Background(), metadata.Request{Kind: metadata.Show, Ref: metadata.Ref{Slug: "26882341-show"}})
	if err == nil || errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("异常HTML不能作为未找到：%v", err)
	}
}

func TestRejectUnsupportedAndCanceledRequests(t *testing.T) {
	provider := New(nil, "https://thetvdb.com", "en", time.Hour)
	for _, req := range []metadata.Request{{Kind: metadata.Movie}, {Kind: metadata.Show, Group: "alternate"}} {
		if _, err := provider.Fetch(context.Background(), req); !errors.Is(err, metadata.ErrUnsupported) {
			t.Fatalf("应拒绝不支持操作：%v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Fetch(ctx, metadata.Request{Kind: metadata.Show, Ref: metadata.Ref{ID: "371065"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("请求未保留取消语义：%v", err)
	}
}

func TestFetchEpisodeUnsupportedBeforeNetwork(t *testing.T) {
	p := New(nil, "http://127.0.0.1:1", "en", 0)
	if p.Supports(metadata.Episode) {
		t.Fatal("单集直取不应声明支持")
	}
	_, err := p.Fetch(context.Background(), metadata.Request{Kind: metadata.Episode, Ref: metadata.Ref{Provider: "thetvdb", Kind: metadata.Show, ID: "371065"}, Season: 1, Episode: 1})
	if !errors.Is(err, metadata.ErrUnsupported) {
		t.Fatalf("单集直取应在网络前返回不支持：%v", err)
	}
}

func TestCanceledRateWaitDoesNotMakeNetworkRequest(t *testing.T) {
	var requests atomic.Int32
	page := fixture(t, "series.html")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); fmt.Fprint(w, page) }))
	defer server.Close()
	provider := New(server.Client(), server.URL, "zh-CN", time.Hour)
	req := metadata.Request{Kind: metadata.Show, Ref: metadata.Ref{Slug: "26882341-show"}}
	if _, err := provider.Fetch(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := provider.Fetch(ctx, req); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("限流等待应可取消：%v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("取消请求仍访问了服务器：%d", requests.Load())
	}
}

func TestRejectExternalRedirect(t *testing.T) {
	var contacted atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted.Store(true) }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }))
	defer server.Close()
	_, err := New(server.Client(), server.URL, "en", 0).Fetch(context.Background(), metadata.Request{Kind: metadata.Show, Ref: metadata.Ref{ID: "371065"}})
	if err == nil || contacted.Load() {
		t.Fatalf("跨站重定向应在请求前拒绝：%v", err)
	}
}

func TestRejectOversizedResponse(t *testing.T) {
	server := servePages(t, map[string]string{"/series/26882341-show": strings.Repeat("x", (8<<20)+1)})
	_, err := New(server.Client(), server.URL, "en", 0).Fetch(context.Background(), metadata.Request{Kind: metadata.Show, Ref: metadata.Ref{Slug: "26882341-show"}})
	if err == nil || errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("超大响应应报独立错误：%v", err)
	}
}

func TestRejectEmptyAllSeasonsStructure(t *testing.T) {
	server := servePages(t, map[string]string{"/series/26882341-show": fixture(t, "series.html"), "/series/26882341-show/allseasons/official": "<h1>All Seasons</h1>"})
	_, err := New(server.Client(), server.URL, "en", 0).FetchSeries(context.Background(), metadata.SeriesRequest{Ref: metadata.Ref{Slug: "26882341-show"}, Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 2}}})
	if err == nil || errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("缺少条目结构不能静默当作未找到：%v", err)
	}
}

func TestAllSeasonsConcurrentCacheAndCanceledWaiter(t *testing.T) {
	page := fixture(t, "allseasons.html")
	started := make(chan struct{})
	release := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
			<-release
		}
		fmt.Fprint(w, page)
	}))
	defer server.Close()
	provider := New(server.Client(), server.URL, "zh-CN", 0)
	target := server.URL + "/series/26882341-show/allseasons/official"
	result := make(chan error, 1)
	go func() {
		entries, err := provider.allSeasons(context.Background(), target, "26882341-show")
		if err == nil && len(entries) != 4 {
			err = fmt.Errorf("单集去重结果错误：%d", len(entries))
		}
		result <- err
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.allSeasons(ctx, target, "26882341-show"); !errors.Is(err, context.Canceled) {
		t.Fatalf("缓存等待必须可取消：%v", err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, err := provider.allSeasons(context.Background(), target, "26882341-show"); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("缓存未复用：%d", requests.Load())
	}
	provider.cacheMu.Lock()
	provider.seasons[target+"|zh-CN"].expires = time.Now().Add(-time.Second)
	provider.cacheMu.Unlock()
	if _, err := provider.allSeasons(context.Background(), target, "26882341-show"); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("过期缓存未重新请求：%d", requests.Load())
	}
}
