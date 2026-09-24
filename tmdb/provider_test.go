package tmdb

import (
	"context"
	"errors"
	"fmt"
	"io"
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

func testMetadataProvider(t *testing.T, handler http.HandlerFunc) metadata.Provider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewMetadataProvider(&config.TmdbConfig{ApiHost: server.URL, ApiKey: "secret-key", ImageHost: "https://images.example", Language: "zh-CN", Rating: "US", TimeoutSeconds: 2})
}

func TestMetadataSearchPreservesErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		notFound bool
	}{
		{"forbidden", 403, `{}`, false},
		{"rate_limit", 429, `{}`, false},
		{"server", 503, `{}`, false},
		{"malformed", 200, `{`, false},
		{"missing", 200, `{"page":1,"total_pages":0,"total_results":0,"results":[]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var count atomic.Int32
			p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			_, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Movie, Title: "电影", Year: 2024})
			if err == nil || errors.Is(err, metadata.ErrNotFound) != tc.notFound {
				t.Fatalf("错误语义不正确：%v", err)
			}
			if strings.Contains(err.Error(), "secret-key") {
				t.Fatalf("泄露密钥：%v", err)
			}
			if !tc.notFound && count.Load() != 1 {
				t.Fatalf("失败后仍尝试其他搜索：%d", count.Load())
			}
		})
	}
}

func TestMetadataSearchReturnsAllUniqueCandidates(t *testing.T) {
	var calls atomic.Int32
	p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/3/search/movie" {
			fmt.Fprint(w, `{"page":1,"total_pages":1,"total_results":3,"results":[{"id":1,"title":"其他","original_title":"Other","release_date":"2010-01-01"},{"id":2,"title":"目标","original_title":"Target","release_date":"2024-01-01"},{"id":1,"title":"重复","release_date":"2010-01-01"}]}`)
		} else {
			fmt.Fprint(w, `{"page":1,"total_pages":1,"total_results":3,"results":[{"id":1,"name":"其他","original_name":"Other","first_air_date":"2010-01-01"},{"id":2,"name":"目标","original_name":"Target","first_air_date":"2024-01-01"},{"id":1,"name":"重复","first_air_date":"2010-01-01"}]}`)
		}
	})
	for _, kind := range []metadata.Kind{metadata.Movie, metadata.Show} {
		got, err := p.Search(context.Background(), metadata.Query{Kind: kind, Title: "目标", OriginalTitle: "Target"})
		if err != nil {
			t.Fatal(err)
		}
		want := []metadata.Candidate{
			{Ref: metadata.Ref{Provider: "tmdb", Kind: kind, ID: "1"}, Title: "其他", OriginalTitle: "Other", Year: 2010},
			{Ref: metadata.Ref{Provider: "tmdb", Kind: kind, ID: "2"}, Title: "目标", OriginalTitle: "Target", Year: 2024},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("应保留全部去重候选及事实：got=%+v want=%+v", got, want)
		}
	}
	if calls.Load() != 4 {
		t.Fatalf("未知年份应完成所有标题且不增加重复查询：%d", calls.Load())
	}
}

func TestMetadataMovieNormalizationAndInstanceImages(t *testing.T) {
	p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("append_to_response"), "release_dates") {
			t.Error("未请求电影分级的官方端点")
		}
		if r.URL.Path != "/3/movie/7" {
			t.Errorf("路径错误：%s", r.URL.Path)
		}
		fmt.Fprint(w, `{"id":7,"imdb_id":"tt7","title":"电影","original_title":"Movie","overview":"简介","release_date":"2020-02-03","status":"Released","runtime":123,"original_language":"zh","spoken_languages":[{"iso_639_1":"en"}],"genres":[{"name":"剧情"}],"production_companies":[{"name":"公司"}],"production_countries":[{"name":"中国","iso_3166_1":"CN"}],"release_dates":{"page":1,"total_pages":1,"total_results":1,"results":[{"iso_3166_1":"US","release_dates":[{"certification":""},{"certification":"PG"}]}]},"credits":{"cast":[{"name":"无头像演员","character":"角色","order":2}],"crew":[{"name":"导演","job":"Director"},{"name":"编剧","department":"Writing","job":"Screenplay"}]},"vote_average":7.5,"vote_count":20,"poster_path":"/poster.jpg","backdrop_path":"/back.jpg","images":{"logos":[{"file_path":"/logo.png"}],"posters":[{"file_path":"/poster.jpg"}]}}`)
	})
	got, err := p.Fetch(context.Background(), metadata.Request{Kind: metadata.Movie, Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Movie, ID: "7"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "电影" || got.OriginalTitle != "Movie" || got.Plot != "简介" || got.Premiered != "2020-02-03" || got.Status != "Released" || got.RuntimeMinutes != 123 || got.Certification != "PG" {
		t.Fatalf("字段丢失：%+v", got)
	}
	if !reflect.DeepEqual(got.Actors, []metadata.Actor{{Name: "无头像演员", Role: "角色", Order: 2}}) || !reflect.DeepEqual(got.Directors, []string{"导演"}) || !reflect.DeepEqual(got.Credits, []string{"编剧"}) {
		t.Fatalf("演职人员错误：%+v", got)
	}
	if !reflect.DeepEqual(got.ExternalIDs, []metadata.Identifier{{Type: "tmdb", Value: "7"}, {Type: "imdb", Value: "tt7"}}) {
		t.Fatalf("标识错误：%+v", got.ExternalIDs)
	}
	if len(got.Artwork) != 3 {
		t.Fatalf("图片重复或丢失：%+v", got.Artwork)
	}
	for _, artwork := range got.Artwork {
		if !strings.HasPrefix(artwork.URL, "https://images.example/t/p/original/") {
			t.Fatalf("图片越过实例边界：%+v", artwork)
		}
	}
	if !reflect.DeepEqual(got.Genres, []string{"剧情"}) || !reflect.DeepEqual(got.Studios, []string{"公司"}) || !reflect.DeepEqual(got.Countries, []string{"中国"}) || !reflect.DeepEqual(got.Languages, []string{"zh", "en"}) {
		t.Fatalf("分类字段丢失：%+v", got)
	}
}

func TestMetadataShowPreservesIdentityAndFields(t *testing.T) {
	p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("append_to_response"), "external_ids") {
			t.Error("未请求同对象外部标识")
		}
		switch r.URL.Path {
		case "/3/tv/9":
			fmt.Fprint(w, `{"id":9,"name":"节目","original_name":"Show","overview":"简介","first_air_date":"2024-01-01","last_air_date":"2024-02-01","status":"Ended","original_language":"en","number_of_seasons":1,"number_of_episodes":2,"networks":[{"name":"电视台"}],"production_companies":[{"name":"公司"}],"content_ratings":{"page":1,"total_pages":1,"total_results":1,"results":[{"iso_3166_1":"US","rating":"TV-PG"}]},"aggregate_credits":{"cast":[{"name":"演员","roles":[],"order":0}],"crew":[{"name":"导演","jobs":[{"job":"Director"}]}]},"seasons":[{"name":"第一季","season_number":1,"poster_path":"/season.jpg"}],"external_ids":{"imdb_id":"ttshow","tvdb_id":99}}`)
		default:
			t.Errorf("路径错误：%s", r.URL.Path)
		}
	})
	ref := metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "9"}
	show, err := p.Fetch(context.Background(), metadata.Request{Kind: metadata.Show, Ref: ref})
	if err != nil {
		t.Fatal(err)
	}
	if len(show.Actors) != 1 || show.Actors[0].Role != "" || show.Actors[0].Thumb != "" || show.Certification != "TV-PG" || show.LastAired != "2024-02-01" || show.SeasonCount != 1 || show.EpisodeCount != 2 {
		t.Fatalf("节目字段错误：%+v", show)
	}
	if !reflect.DeepEqual(show.Studios, []string{"电视台", "公司"}) || !reflect.DeepEqual(show.Seasons, []metadata.Season{{Number: 1, Title: "第一季"}}) {
		t.Fatalf("节目归一化错误：%+v", show)
	}
}

func TestMetadataFetchRejectsWrongReference(t *testing.T) {
	p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) { t.Error("无效引用不应请求网络") })
	for _, req := range []metadata.Request{
		{Kind: metadata.Episode, Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Episode, ID: "9"}, Season: 1, Episode: 1},
		{Kind: metadata.Movie, Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "9"}},
		{Kind: metadata.Show, Ref: metadata.Ref{Provider: "tvdb", Kind: metadata.Show, ID: "9"}},
		{Kind: metadata.Show, Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "abc"}},
		{Kind: metadata.Episode, Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "9"}, Season: 1, Episode: 0},
	} {
		if _, err := p.Fetch(context.Background(), req); err == nil {
			t.Fatalf("无效引用通过：%+v", req)
		}
	}
}

func TestMetadataFetchEpisodeUnsupportedBeforeNetwork(t *testing.T) {
	p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) { t.Error("单集直取不应请求网络") })
	if p.Supports(metadata.Episode) {
		t.Fatal("单集直取不应声明支持")
	}
	_, err := p.Fetch(context.Background(), metadata.Request{Kind: metadata.Episode, Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "9"}, Season: 1, Episode: 1})
	if !errors.Is(err, metadata.ErrUnsupported) {
		t.Fatalf("单集直取应返回不支持：%v", err)
	}
}

func TestMetadataEpisodeGroupsOrderAndIdentity(t *testing.T) {
	var wrongShow atomic.Bool
	p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/3/tv/9":
			fmt.Fprint(w, `{"id":9,"name":"节目","number_of_seasons":99,"number_of_episodes":99}`)
		case "/3/tv/episode_group/group":
			showID := 9
			if wrongShow.Load() {
				showID = 10
			}
			fmt.Fprintf(w, `{"id":"group","name":"分组","group_count":2,"episode_count":3,"groups":[{"name":"后篇","order":2,"episodes":[{"id":903,"season_number":3,"episode_number":1,"show_id":%d,"order":0}]},{"name":"前篇","order":1,"episodes":[{"id":902,"season_number":2,"episode_number":2,"show_id":9,"order":1},{"id":901,"season_number":2,"episode_number":1,"show_id":9,"order":0}]}]}`, showID)
		default:
			t.Errorf("分组映射路径错误：%s", r.URL.Path)
		}
	})
	ref := metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "9"}
	show, err := p.Fetch(context.Background(), metadata.Request{Kind: metadata.Show, Ref: ref, Group: "group"})
	if err != nil {
		t.Fatal(err)
	}
	if show.SeasonCount != 2 || show.EpisodeCount != 3 || !reflect.DeepEqual(show.Seasons, []metadata.Season{{Number: 1, Title: "前篇"}, {Number: 2, Title: "后篇"}}) {
		t.Fatalf("分组节目错误：%+v", show)
	}
	wrongShow.Store(true)
	if _, err := p.Fetch(context.Background(), metadata.Request{Kind: metadata.Show, Ref: ref, Group: "group"}); err == nil {
		t.Fatal("其他节目的分组被接受")
	}
}

func TestMetadataProviderCancellationAndFirstFailure(t *testing.T) {
	t.Run("cancel_request", func(t *testing.T) {
		started := make(chan struct{})
		p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() })
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := p.Search(ctx, metadata.Query{Kind: metadata.Movie, Title: "电影"}); done <- err }()
		<-started
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("取消语义丢失：%v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("取消后请求仍阻塞")
		}
	})
	t.Run("first_failure", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
		defer server.Close()
		p := NewMetadataProvider(&config.TmdbConfig{ApiHost: server.URL, ApiKey: "secret-key"})
		_, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Show, Title: "节目"})
		if err == nil || errors.Is(err, metadata.ErrNotFound) || calls.Load() != 1 {
			t.Fatalf("首次失败未立即返回：%d %v", calls.Load(), err)
		}
	})
}

func TestMetadataSearchRejectsNonSearchPayload(t *testing.T) {
	for _, kind := range []metadata.Kind{metadata.Movie, metadata.Show} {
		for _, body := range []string{
			`{}`, `null`, `{"success":false,"status_code":7}`, `{"results":null}`, `{"results":{}}`,
			`{"page":1,"total_pages":1,"total_results":1,"results":[null]}`, `{"page":1,"total_pages":1,"total_results":1,"results":[{"id":0,"title":"电影","name":"节目"}]}`,
			`{"page":1,"total_pages":1,"total_results":1,"results":[{"id":1}]}`, `{"page":1,"total_pages":1,"total_results":1,"results":[{"id":1,"title":" ","name":" "}]}`,
			`{"page":1,"total_pages":1,"total_results":2,"results":[{"id":1,"title":"电影","name":"节目"},null]}`,
		} {
			p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			_, err := p.Search(context.Background(), metadata.Query{Kind: kind, Title: "作品"})
			if err == nil || errors.Is(err, metadata.ErrNotFound) {
				t.Fatalf("非搜索响应被误判为无候选：%s %s %v", kind, body, err)
			}
		}
	}
}

func TestMetadataSearchKeepsKnownYearConstraintForEveryTitle(t *testing.T) {
	for _, kind := range []metadata.Kind{metadata.Movie, metadata.Show} {
		t.Run(string(kind), func(t *testing.T) {
			var requests []string
			p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
				args := r.URL.Query()
				field := "primary_release_year"
				if kind == metadata.Show {
					field = "first_air_date_year"
				}
				requests = append(requests, args.Get("query")+"|"+args.Get(field))
				if args.Get(field) != "2024" || args.Get("year") != "" {
					t.Errorf("未按作品首发或首播年份查询: %s", r.URL.RawQuery)
				}
				if args.Get("query") != "Target" || args.Get(field) != "" {
					fmt.Fprint(w, `{"page":1,"total_pages":0,"total_results":0,"results":[]}`)
					return
				}
				fmt.Fprint(w, `{"page":1,"total_pages":1,"total_results":1,"results":[{"id":7,"title":"目标","name":"目标","original_title":"Target","original_name":"Target","release_date":"2023-12-31","first_air_date":"2023-12-31"}]}`)
			})
			got, err := p.Search(context.Background(), metadata.Query{Kind: kind, ChineseTitle: "目标", OriginalTitle: "Target", Year: 2024})
			if !errors.Is(err, metadata.ErrNotFound) || len(got) != 0 {
				t.Fatalf("已知年份空结果后仍进行了无年份搜索: %+v %v", got, err)
			}
			want := []string{"目标|2024", "Target|2024"}
			if !reflect.DeepEqual(requests, want) {
				t.Fatalf("查询顺序或年份约束不正确: %v", requests)
			}
		})
	}
}

func TestMetadataSearchUnknownYearDoesNotRestrictReleaseYear(t *testing.T) {
	for _, kind := range []metadata.Kind{metadata.Movie, metadata.Show} {
		t.Run(string(kind), func(t *testing.T) {
			var count int
			p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
				count++
				args := r.URL.Query()
				for _, field := range []string{"year", "primary_release_year", "first_air_date_year"} {
					if args.Has(field) {
						t.Errorf("未知年份仍携带 %s", field)
					}
				}
				fmt.Fprint(w, `{"page":1,"total_pages":1,"total_results":1,"results":[{"id":7,"title":"目标","name":"目标","release_date":"2017-01-01","first_air_date":"2017-01-01"}]}`)
			})
			got, err := p.Search(context.Background(), metadata.Query{Kind: kind, Title: "目标"})
			if err != nil || len(got) != 1 || got[0].Year != 2017 || count != 1 {
				t.Fatalf("未知年份查询不正确: %+v %v 请求=%d", got, err, count)
			}
		})
	}
}

func TestMetadataScopeSeparatesResponseConfiguration(t *testing.T) {
	base := config.TmdbConfig{ApiHost: "https://api.example", ImageHost: "https://images.example", Language: "zh-CN", Rating: "US", ApiKey: "secret-key"}
	scope := NewMetadataProvider(&base).Scope()
	for _, change := range []func(*config.TmdbConfig){
		func(c *config.TmdbConfig) { c.ApiHost = "https://other-api.example" },
		func(c *config.TmdbConfig) { c.ImageHost = "https://other-images.example" },
		func(c *config.TmdbConfig) { c.Language = "en-US" },
		func(c *config.TmdbConfig) { c.Rating = "CN" },
	} {
		settings := base
		change(&settings)
		if NewMetadataProvider(&settings).Scope() == scope {
			t.Fatalf("影响响应的配置未隔离缓存：%+v", settings)
		}
	}
	if strings.Contains(scope, "secret-key") {
		t.Fatalf("缓存范围泄露密钥：%s", scope)
	}
}

func TestMetadataProviderRateLimitFailsWithoutWaiting(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	p := NewMetadataProvider(&config.TmdbConfig{ApiHost: server.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := p.Search(ctx, metadata.Query{Kind: metadata.Show, Title: "节目"})
	var status *statusError
	if !errors.As(err, &status) || status.code != http.StatusTooManyRequests || calls.Load() != 1 {
		t.Fatalf("首次限流错误被退避等待或重试掩盖：请求=%d 错误=%v", calls.Load(), err)
	}
}

func TestMetadataProviderConfigurationIsolated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "original-key" || r.URL.Query().Get("language") != "zh-CN" {
			t.Errorf("实例配置发生变化：%v", r.URL.Query())
		}
		fmt.Fprint(w, `{"id":1,"title":"电影","poster_path":"/one.jpg"}`)
	}))
	defer server.Close()
	settings := &config.TmdbConfig{ApiHost: server.URL, ApiKey: "original-key", ImageHost: "https://one.example", Language: "zh-CN"}
	p := NewMetadataProvider(settings)
	settings.ApiKey, settings.ImageHost = "new-key", "https://two.example"
	q := NewMetadataProvider(settings)
	if p.Scope() == q.Scope() {
		t.Fatal("图片地址不同的实例共享缓存范围")
	}
	settings.ImageHost = "https://one.example"
	r := NewMetadataProvider(settings)
	if p.Scope() != r.Scope() {
		t.Fatal("密钥改变不应改变缓存范围")
	}
	got, err := p.Fetch(context.Background(), metadata.Request{Kind: metadata.Movie, Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Movie, ID: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Artwork) != 1 || got.Artwork[0].URL != "https://one.example/t/p/original/one.jpg" {
		t.Fatalf("实例图片地址发生变化：%+v", got.Artwork)
	}
}

func TestMetadataExternalIDsExcludeEndpointIdentity(t *testing.T) {
	p := testMetadataProvider(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":9,"name":"节目","external_ids":{"id":9,"imdb_id":"tt9","tvdb_id":null,"wikidata_id":"Q9"}}`)
	})
	got, err := p.Fetch(context.Background(), metadata.Request{Kind: metadata.Show, Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Show, ID: "9"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []metadata.Identifier{{Type: "tmdb", Value: "9"}, {Type: "imdb", Value: "tt9"}, {Type: "wikidata", Value: "Q9"}}
	if !reflect.DeepEqual(got.ExternalIDs, want) {
		t.Fatalf("外部标识混入端点对象编号：%+v", got.ExternalIDs)
	}
}

func TestMetadataProvider404SeparatesSearchFailureFromMissingDetails(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	p := NewMetadataProvider(&config.TmdbConfig{ApiHost: server.URL})
	_, searchErr := p.Search(context.Background(), metadata.Query{Kind: metadata.Show, Title: "不存在"})
	if searchErr == nil || errors.Is(searchErr, metadata.ErrNotFound) {
		t.Fatalf("搜索接口404不得归为没有候选：%v", searchErr)
	}
	_, fetchErr := p.Fetch(context.Background(), metadata.Request{Kind: metadata.Movie, Ref: metadata.Ref{Provider: "tmdb", Kind: metadata.Movie, ID: "9"}})
	if !errors.Is(fetchErr, metadata.ErrNotFound) {
		t.Fatalf("404 详情错误未归一化：%v", fetchErr)
	}
	if calls.Load() != 2 {
		t.Fatalf("404 不应重试：%d", calls.Load())
	}
}

func TestMetadataProviderResponseSizeBoundary(t *testing.T) {
	for _, tc := range []struct {
		name         string
		size         int
		wantNotFound bool
	}{
		{"at_limit", 8 * 1024 * 1024, true},
		{"over_limit", 8*1024*1024 + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			const payload = `{"page":1,"total_pages":0,"total_results":0,"results":[]}`
			body := payload + strings.Repeat(" ", tc.size-len(payload))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				io.WriteString(w, body)
			}))
			defer server.Close()
			p := NewMetadataProvider(&config.TmdbConfig{ApiHost: server.URL})
			_, err := p.Search(context.Background(), metadata.Query{Kind: metadata.Movie, Title: "电影"})
			if err == nil || errors.Is(err, metadata.ErrNotFound) != tc.wantNotFound {
				t.Fatalf("响应长度边界错误：%d %v", tc.size, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("过大响应不应重试：%d", calls.Load())
			}
		})
	}
}

func TestMetadataGroupUsesSortedPositionForSparseOrder(t *testing.T) {
	group := &TvEpisodeGroupDetail{Groups: []TvEpisodeGroup{{Order: 2, Episodes: []TvEpisodeGroupEpisode{{Id: 102, Order: 8}, {Id: 101, Order: 3}}}}}
	got, err := groupedEpisode(group, 2, 2)
	if err != nil || got.Id != 102 {
		t.Fatalf("分组集号应是排序后的序位：%+v %v", got, err)
	}
	if group.Groups[0].Episodes[0].Id != 102 {
		t.Fatal("修改了原分组数据")
	}
}
