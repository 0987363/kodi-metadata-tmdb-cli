package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fengqi/kodi-metadata-tmdb-cli/config"

	"fengqi/kodi-metadata-tmdb-cli/metadata"
)

func batchProvider(t *testing.T, handler http.HandlerFunc) metadata.SeriesProvider {
	t.Helper()
	p := testMetadataProvider(t, handler)
	return p.(metadata.SeriesProvider)
}

func batchRef() metadata.Ref {
	return metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "9"}
}

func validBatchObject(path string) any {
	switch {
	case path == "aggregate_credits", strings.HasSuffix(path, "/credits"):
		return map[string]any{"cast": []any{}, "crew": []any{}}
	case path == "content_ratings":
		return map[string]any{"results": []any{}}
	case strings.HasSuffix(path, "/images"):
		return map[string]any{"stills": []any{}}
	case path == "images":
		return map[string]any{"posters": []any{}}
	case path == "external_ids", strings.HasSuffix(path, "/external_ids"):
		return map[string]any{"imdb_id": nil}
	default:
		return nil
	}
}

func TestSeriesBatchSplitsTwentyOnePathsAndReusesFacts(t *testing.T) {
	var requests [][]string
	p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/tv/9" {
			t.Errorf("错误入口：%s", r.URL.Path)
		}
		paths := strings.Split(r.URL.Query().Get("append_to_response"), ",")
		if len(paths) > 20 {
			t.Errorf("追加对象超过 20：%d", len(paths))
		}
		requests = append(requests, paths)
		payload := map[string]any{"id": 9, "name": "节目", "number_of_seasons": 2, "number_of_episodes": 7}
		for _, path := range paths {
			switch path {
			case "aggregate_credits":
				payload[path] = map[string]any{"cast": []any{}, "crew": []any{}}
			case "content_ratings":
				payload[path] = map[string]any{"results": []any{}}
			case "images":
				payload[path] = map[string]any{"posters": []any{}}
			case "external_ids":
				payload[path] = map[string]any{"imdb_id": "tt9"}
			case "season/1", "season/2":
				season := 1
				if path == "season/2" {
					season = 2
				}
				episodes := []any{}
				for number := 1; number <= 7; number++ {
					episodes = append(episodes, map[string]any{"id": season*100 + number, "show_id": 9, "season_number": season, "episode_number": number, "name": fmt.Sprintf("第%d集", number), "runtime": 42})
				}
				payload[path] = map[string]any{"season_number": season, "episodes": episodes}
			default:
				if strings.HasSuffix(path, "/credits") {
					payload[path] = map[string]any{"cast": []any{map[string]any{"name": "演员", "character": "角色"}}, "crew": []any{}}
				} else if strings.HasSuffix(path, "/images") {
					payload[path] = map[string]any{"stills": []any{map[string]any{"file_path": "/still.jpg"}}}
				} else if strings.HasSuffix(path, "/external_ids") {
					payload[path] = map[string]any{"imdb_id": "ttepisode"}
				} else {
					t.Errorf("未知追加路径：%s", path)
				}
			}
		}
		json.NewEncoder(w).Encode(payload)
	})
	keys := []metadata.EpisodeKey{}
	for n := 1; n <= 7; n++ {
		keys = append(keys, metadata.EpisodeKey{Season: 1, Episode: n})
	}
	keys = append(keys, keys[0])
	got, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: keys})
	if err != nil {
		t.Fatal(err)
	}
	if got.Work.Title != "节目" || len(got.Episodes) != 7 || got.Episodes[0].Record.Ref.ID != "101" || got.Episodes[0].Record.RuntimeMinutes != 42 {
		t.Fatalf("基础事实或去重错误：%+v", got)
	}
	if len(requests) != 1 {
		t.Fatalf("基础事实应一次获取：%v", requests)
	}
	got, err = p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: keys, Details: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 3 || len(requests[1]) != 20 || len(requests[2]) != 1 {
		t.Fatalf("21 个扩展对象应分为 20+1：%v", requests)
	}
	if got.Episodes[0].Record.ExternalIDs[1].Value != "ttepisode" || len(got.Episodes[0].Record.Actors) != 1 || len(got.Episodes[0].Record.Artwork) != 1 {
		t.Fatalf("扩展字段丢失：%+v", got.Episodes[0].Record)
	}
	_, err = p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: keys, Details: true})
	if err != nil || len(requests) != 3 {
		t.Fatalf("重复请求未复用事实：%v %v", requests, err)
	}
}

func TestSeriesBatchRejectsMissingAppendedObject(t *testing.T) {
	p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":9,"name":"节目","aggregate_credits":{"cast":[],"crew":[]},"content_ratings":{"results":[]},"images":{"posters":[]},"external_ids":{"imdb_id":null}}`)
	})
	_, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}}})
	if err == nil || !strings.Contains(err.Error(), "season/1") {
		t.Fatalf("HTTP 200 缺少季对象未报错：%v", err)
	}
}

func TestSeriesBatchRejectsEmptyEpisodeRequest(t *testing.T) {
	p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) { t.Error("空单集请求不应访问网络") })
	if _, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef()}); err == nil {
		t.Fatal("空单集请求被接受")
	}
}

func TestSeriesBatchRejectsMissingEpisodeExtension(t *testing.T) {
	p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{"id": 9, "name": "节目"}
		for _, path := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
			if path == "season/1/episode/1/images" {
				continue
			}
			switch path {
			case "season/1":
				payload[path] = map[string]any{"season_number": 1, "episodes": []any{map[string]any{"id": 101, "show_id": 9, "season_number": 1, "episode_number": 1, "name": "单集"}}}
			default:
				payload[path] = validBatchObject(path)
			}
		}
		json.NewEncoder(w).Encode(payload)
	})
	_, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}}, Details: true})
	if err == nil || !strings.Contains(err.Error(), "season/1/episode/1/images") {
		t.Fatalf("缺失扩展对象未报错：%v", err)
	}
}

func TestSeriesBatchRejectsSourceIdentityMismatch(t *testing.T) {
	p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{"id": 9, "name": "节目"}
		for _, path := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
			if path == "season/1" {
				payload[path] = map[string]any{"season_number": 1, "episodes": []any{map[string]any{"id": 101, "show_id": 10, "season_number": 1, "episode_number": 1, "name": "其他节目单集"}}}
			} else {
				payload[path] = validBatchObject(path)
			}
		}
		json.NewEncoder(w).Encode(payload)
	})
	_, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}}})
	if err == nil || !strings.Contains(err.Error(), "身份") {
		t.Fatalf("其他节目单集被接受：%v", err)
	}
}

func TestSeriesBatchRejectsDuplicateCoordinateWithDifferentSourceID(t *testing.T) {
	p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{"id": 9, "name": "节目"}
		for _, path := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
			if path == "season/1" {
				payload[path] = map[string]any{"season_number": 1, "episodes": []any{map[string]any{"id": 101, "show_id": 9, "season_number": 1, "episode_number": 1, "name": "甲"}, map[string]any{"id": 102, "show_id": 9, "season_number": 1, "episode_number": 1, "name": "乙"}}}
			} else {
				payload[path] = validBatchObject(path)
			}
		}
		json.NewEncoder(w).Encode(payload)
	})
	_, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}}})
	if err == nil {
		t.Fatal("同一坐标对应两个来源 ID 被接受")
	}
}

func TestSeriesBatchRejectsDuplicateCoordinateWithSameSourceID(t *testing.T) {
	p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{"id": 9, "name": "节目"}
		for _, path := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
			if path == "season/1" {
				payload[path] = map[string]any{"season_number": 1, "episodes": []any{map[string]any{"id": 101, "show_id": 9, "season_number": 1, "episode_number": 1, "name": "甲"}, map[string]any{"id": 101, "show_id": 9, "season_number": 1, "episode_number": 1, "name": "乙"}}}
			} else {
				payload[path] = validBatchObject(path)
			}
		}
		json.NewEncoder(w).Encode(payload)
	})
	_, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}}})
	if err == nil {
		t.Fatal("同一季集坐标重复出现被接受")
	}
}

func TestSeriesBatchCacheLivesForProviderInstance(t *testing.T) {
	var requests atomic.Int32
	var updated atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		title := "旧标题"
		if updated.Load() {
			title = "新标题"
		}
		payload := map[string]any{"id": 9, "name": "节目"}
		for _, path := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
			if path == "season/1" {
				payload[path] = map[string]any{"season_number": 1, "episodes": []any{map[string]any{"id": 101, "show_id": 9, "season_number": 1, "episode_number": 1, "name": title}}}
			} else {
				payload[path] = validBatchObject(path)
			}
		}
		json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()
	settings := &config.TmdbConfig{ApiHost: server.URL, ApiKey: "test", Language: "zh-CN", TimeoutSeconds: 2}
	first := NewMetadataProvider(settings).(metadata.SeriesProvider)
	req := metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}}}
	old, err := first.FetchSeries(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	updated.Store(true)
	again, err := first.FetchSeries(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || old.Episodes[0].Record.Title != "旧标题" || again.Episodes[0].Record.Title != "旧标题" {
		t.Fatalf("同一实例没有复用事实：count=%d old=%q again=%q", requests.Load(), old.Episodes[0].Record.Title, again.Episodes[0].Record.Title)
	}
	second := NewMetadataProvider(settings).(metadata.SeriesProvider)
	fresh, err := second.FetchSeries(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || fresh.Episodes[0].Record.Title != "新标题" {
		t.Fatalf("新实例未读取更新事实：count=%d title=%q", requests.Load(), fresh.Episodes[0].Record.Title)
	}
}

func TestSeriesBatchCanceledWhileWaitingForProvider(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	var requests atomic.Int32
	p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
			<-release
		}
		payload := map[string]any{"id": 9, "name": "节目"}
		for _, path := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
			if path == "season/1" {
				payload[path] = map[string]any{"season_number": 1, "episodes": []any{map[string]any{"id": 101, "show_id": 9, "season_number": 1, "episode_number": 1, "name": "单集"}}}
			} else {
				payload[path] = validBatchObject(path)
			}
		}
		json.NewEncoder(w).Encode(payload)
	})
	req := metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}}}
	firstDone := make(chan error, 1)
	go func() { _, err := p.FetchSeries(context.Background(), req); firstDone <- err }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() { _, err := p.FetchSeries(ctx, req); secondDone <- err }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("等待取消结果错误：%v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("等待串行请求时取消未及时返回")
	}
}

func TestSeriesBatchMissingEpisodeInCompleteSeasonIsNotFound(t *testing.T) {
	for _, episodes := range []any{[]any{}, []any{map[string]any{"id": 101, "show_id": 9, "season_number": 1, "episode_number": 1, "name": "第一集"}}} {
		p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) {
			payload := map[string]any{"id": 9, "name": "节目"}
			for _, path := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
				if path == "season/1" {
					payload[path] = map[string]any{"season_number": 1, "episodes": episodes}
				} else {
					payload[path] = validBatchObject(path)
				}
			}
			json.NewEncoder(w).Encode(payload)
		})
		_, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 2}}})
		if !errors.Is(err, metadata.ErrNotFound) {
			t.Fatalf("完整季缺集应为不存在：%v", err)
		}
	}
}

func TestSeriesBatchRejectsErrorAndEmptyExtensionObjects(t *testing.T) {
	for _, bad := range []string{`{}`, `{"success":false,"status_code":34,"status_message":"missing"}`} {
		t.Run(bad, func(t *testing.T) {
			p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) {
				payload := map[string]any{"id": 9, "name": "节目"}
				for _, path := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
					switch path {
					case "season/1":
						payload[path] = map[string]any{"season_number": 1, "episodes": []any{map[string]any{"id": 101, "show_id": 9, "season_number": 1, "episode_number": 1, "name": "单集"}}}
					case "season/1/episode/1/credits":
						payload[path] = json.RawMessage(bad)
					default:
						payload[path] = map[string]any{"imdb_id": "tt101", "stills": []any{}, "cast": []any{}, "crew": []any{}, "results": []any{}, "posters": []any{}}
					}
				}
				json.NewEncoder(w).Encode(payload)
			})
			_, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1}}, Details: true})
			if err == nil || !strings.Contains(err.Error(), "/credits") {
				t.Fatalf("无效 credits 对象被接受：%v", err)
			}
		})
	}
}

func TestSeriesBatchAcceptsDifferentGroupsPerKey(t *testing.T) {
	p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/3/tv/episode_group/") {
			id := strings.TrimPrefix(r.URL.Path, "/3/tv/episode_group/")
			season, episodeID := 2, 201
			if id == "later" {
				season, episodeID = 3, 301
			}
			fmt.Fprintf(w, `{"id":%q,"groups":[{"name":"自定义季","order":1,"episodes":[{"id":%d,"show_id":9,"season_number":%d,"episode_number":1,"order":0}]}]}`, id, episodeID, season)
			return
		}
		payload := map[string]any{"id": 9, "name": "节目", "seasons": []any{map[string]any{"season_number": 2, "poster_path": "/wrong.jpg"}}}
		for _, path := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
			switch path {
			case "season/2":
				payload[path] = map[string]any{"season_number": 2, "episodes": []any{map[string]any{"id": 201, "show_id": 9, "season_number": 2, "episode_number": 1, "name": "前篇"}}}
			case "season/3":
				payload[path] = map[string]any{"season_number": 3, "episodes": []any{map[string]any{"id": 301, "show_id": 9, "season_number": 3, "episode_number": 1, "name": "后篇"}}}
			default:
				payload[path] = map[string]any{"imdb_id": "tt9", "stills": []any{}, "cast": []any{}, "crew": []any{}, "results": []any{}, "posters": []any{}}
			}
		}
		json.NewEncoder(w).Encode(payload)
	})
	keys := []metadata.EpisodeKey{{Season: 1, Episode: 1, Group: "early"}, {Season: 1, Episode: 1, Group: "later"}}
	got, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: keys})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Episodes) != 2 || got.Episodes[0].Record.Ref.ID != "201" || got.Episodes[1].Record.Ref.ID != "301" {
		t.Fatalf("不同分组映射错误：%+v", got.Episodes)
	}
	if len(got.Work.Seasons) != 0 {
		t.Fatalf("混合分组没有统一季命名：%+v", got.Work.Seasons)
	}
	for _, art := range got.Work.Artwork {
		if art.Kind == "season_poster" {
			t.Fatalf("混合分组复用了原季海报：%+v", art)
		}
	}
}

func TestSeriesBatchFetchesMultipleSeasonsInOneRootRequest(t *testing.T) {
	var requests [][]string
	p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) {
		paths := strings.Split(r.URL.Query().Get("append_to_response"), ",")
		requests = append(requests, paths)
		payload := map[string]any{"id": 9, "name": "节目"}
		for _, path := range paths {
			switch path {
			case "season/1":
				payload[path] = map[string]any{"season_number": 1, "episodes": []any{map[string]any{"id": 101, "show_id": 9, "season_number": 1, "episode_number": 1, "name": "第一季"}}}
			case "season/2":
				payload[path] = map[string]any{"season_number": 2, "episodes": []any{map[string]any{"id": 201, "show_id": 9, "season_number": 2, "episode_number": 1, "name": "第二季"}}}
			default:
				payload[path] = validBatchObject(path)
			}
		}
		json.NewEncoder(w).Encode(payload)
	})
	got, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{{Season: 2, Episode: 1}, {Season: 1, Episode: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || got.Episodes[0].Record.Ref.ID != "201" || got.Episodes[1].Record.Ref.ID != "101" {
		t.Fatalf("跨季批量或结果顺序错误：%v %+v", requests, got.Episodes)
	}
}

func TestSeriesBatchPreservesGroupedSourceIdentityAndCoordinates(t *testing.T) {
	p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/3/tv/episode_group/group" {
			fmt.Fprint(w, `{"id":"group","groups":[{"name":"编排季","order":1,"episodes":[{"id":201,"show_id":9,"season_number":2,"episode_number":1,"order":0}]}]}`)
			return
		}
		payload := map[string]any{"id": 9, "name": "节目", "seasons": []any{map[string]any{"season_number": 2, "poster_path": "/wrong.jpg"}}}
		for _, path := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
			switch path {
			case "season/2":
				payload[path] = map[string]any{"season_number": 2, "episodes": []any{map[string]any{"id": 201, "show_id": 9, "season_number": 2, "episode_number": 1, "name": "原集"}}}
			case "aggregate_credits", "content_ratings", "images", "external_ids":
				payload[path] = validBatchObject(path)
			default:
				t.Errorf("未知路径：%s", path)
			}
		}
		json.NewEncoder(w).Encode(payload)
	})
	key := metadata.EpisodeKey{Season: 1, Episode: 1, Group: "group"}
	got, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{key}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Episodes) != 1 || !reflect.DeepEqual(got.Episodes[0].Key, key) || got.Episodes[0].Record.Ref.ID != "201" || got.Episodes[0].Record.SeasonNumber != 1 || got.Episodes[0].Record.EpisodeNumber != 1 || !strings.Contains(got.Episodes[0].Record.SourceURL, "/season/2/episode/1") {
		t.Fatalf("分组映射错误：%+v", got)
	}
	for _, art := range got.Work.Artwork {
		if art.Kind == "season_poster" {
			t.Fatalf("原季海报错误映射到分组季：%+v", art)
		}
	}
	for _, bounds := range [][2]int{{3, 1}, {1, 3}, {-1, 1}, {1, 0}} {
		_, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{{Season: bounds[0], Episode: bounds[1], Group: "group"}}})
		if err == nil {
			t.Fatalf("分组越界未拒绝：%v", bounds)
		}
	}
}

func TestSeriesBatchDeduplicatesSharedSourceEpisodePaths(t *testing.T) {
	var extensionPaths []string
	p := batchProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/3/tv/episode_group/group" {
			fmt.Fprint(w, `{"id":"group","groups":[{"name":"编排季","order":1,"episodes":[{"id":201,"show_id":9,"season_number":2,"episode_number":1,"order":0},{"id":201,"show_id":9,"season_number":2,"episode_number":1,"order":1}]}]}`)
			return
		}
		payload := map[string]any{"id": 9, "name": "节目"}
		for _, path := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
			if strings.Contains(path, "/episode/") {
				extensionPaths = append(extensionPaths, path)
			}
			switch path {
			case "season/2":
				payload[path] = map[string]any{"season_number": 2, "episodes": []any{map[string]any{"id": 201, "show_id": 9, "season_number": 2, "episode_number": 1, "name": "原集"}}}
			default:
				payload[path] = validBatchObject(path)
			}
		}
		json.NewEncoder(w).Encode(payload)
	})
	got, err := p.FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{{Season: 1, Episode: 1, Group: "group"}, {Season: 1, Episode: 2, Group: "group"}}, Details: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Episodes) != 2 || len(extensionPaths) != 3 {
		t.Fatalf("共享来源单集重复请求扩展事实：%v %+v", extensionPaths, got.Episodes)
	}
}

func TestSeriesBatchDetailsPreserveEpisodeRecord(t *testing.T) {
	const episode = `{"id":101,"show_id":9,"season_number":1,"episode_number":1,"name":"单集","overview":"简介","air_date":"2024-01-02","runtime":42,"still_path":"/still.jpg","guest_stars":[{"name":"客串","character":"本人"}],"crew":[{"name":"导演","job":"Director"}],"credits":{"cast":[{"name":"主演","character":"角色","order":0}],"crew":[]},"images":{"stills":[{"file_path":"/extra.jpg"}]},"external_ids":{"imdb_id":"tt101","tvdb_id":999}}`
	p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/tv/9" {
			t.Errorf("请求了错误入口：%s", r.URL.Path)
		}
		payload := map[string]any{"id": 9, "name": "节目"}
		for _, path := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
			switch path {
			case "season/1":
				payload[path] = json.RawMessage(`{"season_number":1,"episodes":[` + episode[:strings.Index(episode, `,"credits"`)] + `}]}`)
			case "season/1/episode/1/credits":
				payload[path] = json.RawMessage(`{"cast":[{"name":"主演","character":"角色","order":0}],"crew":[]}`)
			case "season/1/episode/1/images":
				payload[path] = json.RawMessage(`{"stills":[{"file_path":"/extra.jpg"}]}`)
			case "season/1/episode/1/external_ids":
				payload[path] = json.RawMessage(`{"imdb_id":"tt101","tvdb_id":999}`)
			default:
				payload[path] = validBatchObject(path)
			}
		}
		json.NewEncoder(w).Encode(payload)
	})
	key := metadata.EpisodeKey{Season: 1, Episode: 1}
	batch, err := p.(metadata.SeriesProvider).FetchSeries(context.Background(), metadata.SeriesRequest{Ref: batchRef(), Episodes: []metadata.EpisodeKey{key}, Details: true})
	if err != nil {
		t.Fatal(err)
	}
	record := batch.Episodes[0].Record
	if record.Ref.Kind != metadata.Episode || record.Ref.ID != "101" || record.SeasonNumber != 1 || record.EpisodeNumber != 1 || record.RuntimeMinutes != 42 || record.Plot != "简介" || !strings.Contains(record.SourceURL, "/season/1/episode/1") {
		t.Fatalf("批量单集身份或字段错误：%+v", record)
	}
	if !reflect.DeepEqual(record.ExternalIDs, []metadata.Identifier{{Type: "tmdb", Value: "101"}, {Type: "imdb", Value: "tt101"}, {Type: "tvdb", Value: "999"}}) || len(record.Actors) == 0 || len(record.Artwork) < 2 {
		t.Fatalf("批量单集扩展字段错误：%+v", record)
	}
}
